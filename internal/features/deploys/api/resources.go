package deploys

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
	yaml "gopkg.in/yaml.v3"
)

type secretProvider struct {
	ID     string                 `json:"id"`
	Type   string                 `json:"type"`
	Config map[string]interface{} `json:"config"`
}

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
	if len(raw) == 0 {
		return nil
	}
	provider := &secretProvider{}
	if id, ok := raw["id"].(string); ok {
		provider.ID = id
	}
	if provider.ID == "" {
		if id, ok := raw["_id"].(string); ok {
			provider.ID = id
		}
	}
	if typ, ok := raw["type"].(string); ok {
		provider.Type = typ
	}
	if config, ok := raw["config"].(bson.M); ok {
		provider.Config = map[string]interface{}(config)
	}
	if provider.Config == nil {
		if config, ok := raw["config"].(map[string]interface{}); ok {
			provider.Config = config
		}
	}
	if provider.Config == nil {
		provider.Config = map[string]interface{}{}
	}
	return provider
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
	return strings.HasPrefix(value, "vault://") ||
		strings.HasPrefix(value, "aws://") ||
		strings.HasPrefix(value, "gcp://") ||
		strings.HasPrefix(value, "secret://")
}

func resolveSecretValue(ctx context.Context, provider *secretProvider, environment, value string) (string, error) {
	value = strings.ReplaceAll(value, "{{env}}", environment)
	if strings.HasPrefix(value, "secret://") {
		if provider == nil {
			return "", errors.New("secret provider not configured")
		}
		value = strings.Replace(value, "secret://", provider.Type+"://", 1)
	}
	if strings.HasPrefix(value, "vault://") {
		if provider == nil || provider.Type != "vault" {
			return "", errors.New("vault provider not configured")
		}
		return resolveVaultSecret(ctx, provider, strings.TrimPrefix(value, "vault://"))
	}
	if strings.HasPrefix(value, "aws://") {
		if provider == nil || provider.Type != "aws" {
			return "", errors.New("aws provider not configured")
		}
		return resolveAwsSecret(ctx, provider, strings.TrimPrefix(value, "aws://"))
	}
	if strings.HasPrefix(value, "gcp://") {
		if provider == nil || provider.Type != "gcp" {
			return "", errors.New("gcp provider not configured")
		}
		return resolveGcpSecret(ctx, provider, strings.TrimPrefix(value, "gcp://"))
	}
	return "", errors.New("unsupported secret reference")
}

func resolveVaultSecret(ctx context.Context, provider *secretProvider, ref string) (string, error) {
	address := strings.TrimSpace(mapStringValue(provider.Config, "address"))
	token := strings.TrimSpace(mapStringValue(provider.Config, "token"))
	if address == "" || token == "" {
		return "", errors.New("vault address/token missing")
	}
	path, key := splitSecretRef(ref)
	if path == "" {
		return "", errors.New("vault secret path missing")
	}
	endpoint := strings.TrimRight(address, "/") + "/v1/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("vault request failed: %s", resp.Status)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	data := mapValue(payload["data"])
	if nested := mapValue(data["data"]); len(nested) > 0 {
		data = nested
	}
	return extractSecretValue(data, key)
}

func resolveAwsSecret(ctx context.Context, provider *secretProvider, ref string) (string, error) {
	accessKey := mapStringValue(provider.Config, "accessKeyId")
	secretKey := mapStringValue(provider.Config, "secretAccessKey")
	region := mapStringValue(provider.Config, "region")
	if region == "" {
		region = "us-east-1"
	}
	if accessKey == "" || secretKey == "" {
		return "", errors.New("aws credentials missing")
	}
	secretName, jsonKey := splitSecretRef(ref)
	if secretName == "" {
		return "", errors.New("aws secret name missing")
	}
	endpoint := fmt.Sprintf("https://secretsmanager.%s.amazonaws.com/", region)
	body := map[string]string{"SecretId": secretName}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := time.Now().UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)

	signedHeaders, authHeader, err := signAwsRequest(req, payload, accessKey, secretKey, region, "secretsmanager", amzDate, dateStamp)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-Amz-Content-Sha256", hashSHA256(payload))
	req.Header.Set("SignedHeaders", signedHeaders)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("aws secret request failed: %s", resp.Status)
	}
	var response struct {
		SecretString string `json:"SecretString"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	if response.SecretString == "" {
		return "", errors.New("aws secret payload empty")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(response.SecretString), &data); err != nil {
		return "", err
	}
	return extractSecretValue(data, jsonKey)
}

func resolveGcpSecret(ctx context.Context, provider *secretProvider, ref string) (string, error) {
	serviceAccount := mapStringValue(provider.Config, "serviceAccountJson")
	projectID := mapStringValue(provider.Config, "projectId")
	secretRef, version := splitSecretRef(ref)
	if secretRef == "" {
		return "", errors.New("gcp secret name missing")
	}
	if strings.Contains(secretRef, "/") {
		parts := strings.SplitN(secretRef, "/", 2)
		projectID = parts[0]
		secretRef = parts[1]
	}
	if projectID == "" {
		return "", errors.New("gcp project id missing")
	}
	if version == "" {
		version = "latest"
	}
	token, err := fetchGcpAccessToken(ctx, serviceAccount)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("https://secretmanager.googleapis.com/v1/projects/%s/secrets/%s/versions/%s:access", projectID, secretRef, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gcp secret request failed: %s", resp.Status)
	}
	var response struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	if response.Payload.Data == "" {
		return "", errors.New("gcp secret payload empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(response.Payload.Data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func fetchGcpAccessToken(ctx context.Context, serviceAccountJSON string) (string, error) {
	if serviceAccountJSON == "" {
		return "", errors.New("gcp service account json missing")
	}
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(serviceAccountJSON), &sa); err != nil {
		return "", err
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return "", errors.New("gcp service account invalid")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	iat := time.Now().Unix()
	exp := iat + 3600
	claims := map[string]interface{}{
		"iss":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   sa.TokenURI,
		"iat":   iat,
		"exp":   exp,
	}
	jwt, err := signJwt(claims, sa.PrivateKey)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gcp token request failed: %s", resp.Status)
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", errors.New("gcp access token missing")
	}
	return response.AccessToken, nil
}

func signJwt(claims map[string]interface{}, privateKeyPEM string) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := encodedHeader + "." + encodedClaims

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", errors.New("invalid private key")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		privKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", err
		}
	}
	rsaKey, ok := privKey.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("private key is not RSA")
	}
	hashed := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	encodedSig := base64.RawURLEncoding.EncodeToString(signature)
	return unsigned + "." + encodedSig, nil
}

func signAwsRequest(req *http.Request, payload []byte, accessKey, secretKey, region, service, amzDate, dateStamp string) (string, string, error) {
	canonicalURI := "/"
	canonicalQuery := ""
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-amz-date:%s\nx-amz-target:%s\n",
		req.Header.Get("Content-Type"),
		req.URL.Host,
		amzDate,
		req.Header.Get("X-Amz-Target"),
	)
	signedHeaders := "content-type;host;x-amz-date;x-amz-target"
	payloadHash := hashSHA256(payload)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	algorithm := "AWS4-HMAC-SHA256"
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		credentialScope,
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveAwsSigningKey(secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, accessKey, credentialScope, signedHeaders, signature)

	return signedHeaders, authorization, nil
}

func deriveAwsSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hashSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func splitSecretRef(ref string) (string, string) {
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return ref, ""
}

func mapValue(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case bson.M:
		return map[string]interface{}(v)
	default:
		return map[string]interface{}{}
	}
}

func extractSecretValue(data map[string]interface{}, key string) (string, error) {
	if key == "" {
		if len(data) == 1 {
			for _, value := range data {
				return fmt.Sprint(value), nil
			}
		}
		if value, ok := data["value"]; ok {
			return fmt.Sprint(value), nil
		}
		return "", errors.New("secret key required")
	}
	value, ok := data[key]
	if !ok {
		return "", errors.New("secret key not found")
	}
	return fmt.Sprint(value), nil
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
