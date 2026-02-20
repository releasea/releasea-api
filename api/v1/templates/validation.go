package templates

import (
	"fmt"
	"strings"

	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func normalizeTemplatePayload(payload bson.M, isCreate bool) (bson.M, error) {
	template := bson.M{}
	for key, value := range payload {
		template[key] = value
	}

	label := strings.TrimSpace(shared.StringValue(template["label"]))
	if label == "" {
		return nil, fmt.Errorf("template label required")
	}
	template["label"] = label

	templateType := strings.ToLower(strings.TrimSpace(shared.StringValue(template["type"])))
	if templateType == "" {
		return nil, fmt.Errorf("template type required")
	}
	if templateType != "microservice" && templateType != "static-site" {
		return nil, fmt.Errorf("template type must be microservice or static-site")
	}
	template["type"] = templateType

	repoMode := strings.ToLower(strings.TrimSpace(shared.StringValue(template["repoMode"])))
	if repoMode == "" {
		repoMode = "existing"
	}
	if repoMode != "existing" && repoMode != "template" {
		return nil, fmt.Errorf("repoMode must be existing or template")
	}
	template["repoMode"] = repoMode

	if defaultsRaw, ok := template["templateDefaults"]; ok {
		if _, ok := normalizeMap(defaultsRaw); !ok {
			return nil, fmt.Errorf("templateDefaults must be an object")
		}
	}

	if sourceRaw, ok := template["templateSource"]; ok {
		source, valid := normalizeMap(sourceRaw)
		if !valid {
			return nil, fmt.Errorf("templateSource must be an object")
		}
		if owner := strings.TrimSpace(shared.StringValue(source["owner"])); owner != "" {
			source["owner"] = owner
		}
		if repo := strings.TrimSpace(shared.StringValue(source["repo"])); repo != "" {
			source["repo"] = repo
		}
		if path := strings.Trim(strings.TrimSpace(shared.StringValue(source["path"])), "/"); path != "" {
			source["path"] = path
		}
		template["templateSource"] = source
	}

	if repoMode == "template" {
		sourceRaw, hasSource := template["templateSource"]
		if !hasSource {
			return nil, fmt.Errorf("templateSource required for template repo mode")
		}
		source, valid := normalizeMap(sourceRaw)
		if !valid || strings.TrimSpace(shared.StringValue(source["owner"])) == "" || strings.TrimSpace(shared.StringValue(source["repo"])) == "" {
			return nil, fmt.Errorf("templateSource owner and repo are required")
		}
	}

	if highlightsRaw, exists := template["highlights"]; exists {
		highlights := shared.ToStringSlice(highlightsRaw)
		template["highlights"] = highlights
	}

	if resourcesRaw, exists := template["resources"]; exists {
		resources, err := normalizeResources(resourcesRaw)
		if err != nil {
			return nil, err
		}
		template["resources"] = resources
	}

	if schemaVersion := strings.TrimSpace(shared.StringValue(template["schemaVersion"])); schemaVersion != "" {
		template["schemaVersion"] = schemaVersion
	} else {
		template["schemaVersion"] = "v1"
	}

	if isCreate {
		template["createdAt"] = shared.NowISO()
	}
	template["updatedAt"] = shared.NowISO()
	return template, nil
}

func normalizeMap(value interface{}) (map[string]interface{}, bool) {
	switch v := value.(type) {
	case map[string]interface{}:
		return v, true
	case bson.M:
		return map[string]interface{}(v), true
	default:
		return nil, false
	}
}

func normalizeResources(value interface{}) ([]map[string]interface{}, error) {
	items, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]map[string]interface{}); ok {
			items = make([]interface{}, 0, len(typed))
			for _, resource := range typed {
				items = append(items, resource)
			}
		} else {
			return nil, fmt.Errorf("resources must be an array")
		}
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		resource, ok := normalizeMap(item)
		if !ok {
			return nil, fmt.Errorf("resources entries must be objects")
		}
		kind := strings.TrimSpace(shared.StringValue(resource["kind"]))
		if kind == "" {
			return nil, fmt.Errorf("resources entries require kind")
		}
		out = append(out, resource)
	}
	return out, nil
}
