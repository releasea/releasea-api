package deploys

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	secretsproviders "releaseaapi/internal/platform/providers/secrets"
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
	yaml "gopkg.in/yaml.v3"
)

type secretProvider = secretsproviders.Provider

func BuildDeployResources(ctx context.Context, service bson.M, environment string) ([]map[string]interface{}, error) {
	template, err := ResolveDeployTemplate(ctx, service)
	if err != nil {
		return nil, err
	}
	if len(template) == 0 {
		return nil, errors.New("deploy template missing")
	}

	resources, err := ExtractTemplateResources(template)
	if err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return nil, errors.New("deploy template resources empty")
	}

	secretProvider, _ := ResolveSecretProvider(ctx, service)
	provider := parseSecretProvider(secretProvider)

	serviceName := shared.ToKubeName(mapStringValue(service, "name"))
	if serviceName == "" {
		serviceName = shared.ToKubeName(mapStringValue(service, "id"))
	}
	if serviceName == "" {
		return nil, errors.New("service name invalid")
	}

	namespace := shared.ResolveAppNamespace(environment)
	if err := shared.ValidateAppNamespace(namespace); err != nil {
		return nil, fmt.Errorf("deploy blocked: %w", err)
	}
	internalDomain := shared.EnvOrDefault("RELEASEA_INTERNAL_DOMAIN", "releasea.internal")
	externalDomain := shared.EnvOrDefault("RELEASEA_EXTERNAL_DOMAIN", "releasea.external")
	internalGateway := shared.EnvOrDefault("RELEASEA_INTERNAL_GATEWAY", "istio-system/releasea-internal-gateway")
	externalGateway := shared.EnvOrDefault("RELEASEA_EXTERNAL_GATEWAY", "istio-system/releasea-external-gateway")

	plainEnv, secretEnv, err := resolveEnvVars(ctx, provider, mapStringMap(service, "environment"), environment)
	if err != nil {
		return nil, err
	}

	replacements := map[string]string{
		"serviceName":      serviceName,
		"namespace":        namespace,
		"image":            mapStringValue(service, "dockerImage"),
		"port":             strconv.Itoa(resolvePortValue(service["port"])),
		"healthCheckPath":  strings.TrimSpace(mapStringValue(service, "healthCheckPath")),
		"internalHost":     fmt.Sprintf("%s.%s", serviceName, internalDomain),
		"externalHost":     fmt.Sprintf("%s.%s", serviceName, externalDomain),
		"internalGateway":  internalGateway,
		"externalGateway":  externalGateway,
		"scheduleCron":     strings.TrimSpace(mapStringValue(service, "scheduleCron")),
		"scheduleTimezone": strings.TrimSpace(mapStringValue(service, "scheduleTimezone")),
		"scheduleCommand":  strings.TrimSpace(mapStringValue(service, "scheduleCommand")),
		"scheduleRetries":  defaultNumericString(mapStringValue(service, "scheduleRetries"), "0"),
		"scheduleTimeout":  defaultNumericString(mapStringValue(service, "scheduleTimeout"), "0"),
	}

	output := make([]map[string]interface{}, 0)
	if len(secretEnv) > 0 {
		output = append(output, buildSecretResource(serviceName, namespace, secretEnv))
	}

	for _, resource := range resources {
		rendered := renderTemplateResource(resource, replacements)
		rendered = normalizeResourceNumbers(rendered)
		scrubCronJobResource(rendered, replacements)
		applyHealthCheckProbes(rendered, replacements["healthCheckPath"], resolvePortValue(service["port"]))
		if kind, _ := rendered["kind"].(string); strings.EqualFold(kind, "VirtualService") {
			continue
		}
		if err := injectEnvVars(rendered, plainEnv, secretEnv, serviceName); err != nil {
			return nil, err
		}
		output = append(output, rendered)
	}

	return output, nil
}

func applyHealthCheckProbes(resource map[string]interface{}, rawPath string, port int) {
	if port <= 0 {
		return
	}
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	kind, _ := resource["kind"].(string)
	if !strings.EqualFold(kind, "Deployment") {
		return
	}
	spec, ok := resource["spec"].(map[string]interface{})
	if !ok {
		return
	}
	template, ok := spec["template"].(map[string]interface{})
	if !ok {
		return
	}
	podSpec, ok := template["spec"].(map[string]interface{})
	if !ok {
		return
	}
	containers, ok := podSpec["containers"].([]interface{})
	if !ok {
		return
	}
	for _, item := range containers {
		container, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, exists := container["readinessProbe"]; !exists {
			container["readinessProbe"] = map[string]interface{}{
				"httpGet": map[string]interface{}{
					"path": path,
					"port": port,
				},
				"initialDelaySeconds": 5,
				"periodSeconds":       10,
				"timeoutSeconds":      2,
				"failureThreshold":    3,
			}
		}
		if _, exists := container["livenessProbe"]; !exists {
			container["livenessProbe"] = map[string]interface{}{
				"httpGet": map[string]interface{}{
					"path": path,
					"port": port,
				},
				"initialDelaySeconds": 15,
				"periodSeconds":       20,
				"timeoutSeconds":      2,
				"failureThreshold":    3,
			}
		}
	}
}

func BuildNamespaceResource(namespace string) map[string]interface{} {
	if namespace == "" {
		namespace = "releasea-apps-prod"
	}
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": namespace,
			"labels": map[string]interface{}{
				"istio-injection": "enabled",
			},
		},
	}
}

func RenderResourcesYAML(resources []map[string]interface{}) (string, error) {
	if len(resources) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		resource = normalizeResourceNumbers(resource)
		out, err := yaml.Marshal(resource)
		if err != nil {
			return "", err
		}
		if len(out) == 0 {
			continue
		}
		parts = append(parts, strings.TrimSpace(string(out)))
	}
	return strings.Join(parts, "\n---\n"), nil
}

func resolvePortValue(value interface{}) int {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case float32:
		if v > 0 {
			return int(v)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 80
}

func ExtractTemplateResources(template bson.M) ([]map[string]interface{}, error) {
	raw := template["resources"]
	if raw == nil {
		return nil, nil
	}
	var items []interface{}
	switch value := raw.(type) {
	case []interface{}:
		items = value
	case bson.A:
		items = []interface{}(value)
	case []bson.M:
		items = make([]interface{}, len(value))
		for i, item := range value {
			items[i] = item
		}
	case []map[string]interface{}:
		items = make([]interface{}, len(value))
		for i, item := range value {
			items[i] = item
		}
	default:
		return nil, errors.New("invalid deploy template resources")
	}
	if len(items) == 0 {
		return nil, nil
	}
	resources := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case bson.M:
			resources = append(resources, map[string]interface{}(value))
		case map[string]interface{}:
			resources = append(resources, value)
		default:
			return nil, errors.New("invalid deploy template resource")
		}
	}
	return resources, nil
}

func parseSecretProvider(raw bson.M) *secretProvider {
	return secretsproviders.ParseProvider(raw)
}

func renderTemplateResource(resource map[string]interface{}, replacements map[string]string) map[string]interface{} {
	if resource == nil {
		return map[string]interface{}{}
	}
	rendered := renderValue(resource, replacements)
	if out, ok := rendered.(map[string]interface{}); ok {
		return out
	}
	return map[string]interface{}{}
}

func renderValue(value interface{}, replacements map[string]string) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, val := range v {
			out[key] = renderValue(val, replacements)
		}
		return out
	case bson.M:
		out := make(map[string]interface{}, len(v))
		for key, val := range v {
			out[key] = renderValue(val, replacements)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = renderValue(item, replacements)
		}
		return out
	case bson.A:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = renderValue(item, replacements)
		}
		return out
	case string:
		result := v
		for key, replacement := range replacements {
			result = strings.ReplaceAll(result, "{{"+key+"}}", replacement)
		}
		return result
	default:
		return value
	}
}

func normalizeResourceNumbers(value interface{}) map[string]interface{} {
	root, ok := value.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	normalizeNumbers(root)
	return root
}

func defaultNumericString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func scrubCronJobResource(resource map[string]interface{}, replacements map[string]string) {
	kind, _ := resource["kind"].(string)
	if !strings.EqualFold(kind, "CronJob") {
		return
	}
	spec, ok := resource["spec"].(map[string]interface{})
	if !ok {
		return
	}
	if strings.TrimSpace(replacements["scheduleTimezone"]) == "" {
		delete(spec, "timeZone")
	}
	if strings.TrimSpace(replacements["scheduleCommand"]) == "" {
		jobTemplate, ok := spec["jobTemplate"].(map[string]interface{})
		if !ok {
			return
		}
		jobSpec, ok := jobTemplate["spec"].(map[string]interface{})
		if !ok {
			return
		}
		template, ok := jobSpec["template"].(map[string]interface{})
		if !ok {
			return
		}
		podSpec, ok := template["spec"].(map[string]interface{})
		if !ok {
			return
		}
		containers, ok := podSpec["containers"].([]interface{})
		if !ok {
			return
		}
		for _, item := range containers {
			container, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			delete(container, "command")
		}
	}
}

func normalizeNumbers(value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, item := range v {
			if shouldCoerceNumeric(key) {
				if str, ok := item.(string); ok {
					if parsed, err := strconv.Atoi(str); err == nil {
						v[key] = parsed
						continue
					}
				}
			}
			normalizeNumbers(item)
		}
	case bson.M:
		for key, item := range v {
			if shouldCoerceNumeric(key) {
				if str, ok := item.(string); ok {
					if parsed, err := strconv.Atoi(str); err == nil {
						v[key] = parsed
						continue
					}
				}
			}
			normalizeNumbers(item)
		}
	case []interface{}:
		for _, item := range v {
			normalizeNumbers(item)
		}
	case bson.A:
		for _, item := range v {
			normalizeNumbers(item)
		}
	}
}

func shouldCoerceNumeric(key string) bool {
	switch key {
	case "port", "containerPort", "targetPort", "number", "replicas", "backoffLimit", "activeDeadlineSeconds":
		return true
	default:
		return false
	}
}

func resolveEnvVars(ctx context.Context, provider *secretProvider, env map[string]string, environment string) (map[string]string, map[string]string, error) {
	plain := map[string]string{}
	secret := map[string]string{}
	for key, value := range env {
		if key == "" || value == "" {
			continue
		}
		if isSecretRef(value) {
			resolved, err := resolveSecretValue(ctx, provider, environment, value)
			if err != nil {
				return nil, nil, err
			}
			secret[key] = resolved
			continue
		}
		plain[key] = value
	}
	return plain, secret, nil
}

func isSecretRef(value string) bool {
	return secretsproviders.IsSecretRef(value)
}

func resolveSecretValue(ctx context.Context, provider *secretProvider, environment, value string) (string, error) {
	return secretsproviders.ResolveReference(ctx, provider, environment, value)
}

func mapStringValue(config map[string]interface{}, key string) string {
	if config == nil {
		return ""
	}
	value, ok := config[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func mapStringMap(config bson.M, key string) map[string]string {
	raw, ok := config[key]
	if !ok || raw == nil {
		return map[string]string{}
	}
	switch v := raw.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k] = fmt.Sprint(val)
		}
		return out
	case bson.M:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k] = fmt.Sprint(val)
		}
		return out
	default:
		return map[string]string{}
	}
}

func buildSecretResource(serviceName, namespace string, secrets map[string]string) map[string]interface{} {
	stringData := map[string]interface{}{}
	for key, value := range secrets {
		stringData[key] = value
	}
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name":      serviceName + "-secrets",
			"namespace": namespace,
		},
		"type":       "Opaque",
		"stringData": stringData,
	}
}

func injectEnvVars(resource map[string]interface{}, plainEnv, secretEnv map[string]string, secretName string) error {
	if len(plainEnv) == 0 && len(secretEnv) == 0 {
		return nil
	}
	kind, _ := resource["kind"].(string)
	if kind != "Deployment" {
		return nil
	}
	spec, _ := resource["spec"].(map[string]interface{})
	template, _ := spec["template"].(map[string]interface{})
	templateSpec, _ := template["spec"].(map[string]interface{})
	containers, _ := templateSpec["containers"].([]interface{})
	if len(containers) == 0 {
		return nil
	}
	container, ok := containers[0].(map[string]interface{})
	if !ok {
		return nil
	}
	envList := make([]interface{}, 0, len(plainEnv)+len(secretEnv))
	for key, value := range plainEnv {
		if key == "" {
			continue
		}
		envList = append(envList, map[string]interface{}{
			"name":  key,
			"value": value,
		})
	}
	for key := range secretEnv {
		if key == "" {
			continue
		}
		envList = append(envList, map[string]interface{}{
			"name": key,
			"valueFrom": map[string]interface{}{
				"secretKeyRef": map[string]interface{}{
					"name": secretName + "-secrets",
					"key":  key,
				},
			},
		})
	}
	if len(envList) == 0 {
		return nil
	}
	container["env"] = envList
	containers[0] = container
	templateSpec["containers"] = containers
	template["spec"] = templateSpec
	spec["template"] = template
	resource["spec"] = spec
	return nil
}
