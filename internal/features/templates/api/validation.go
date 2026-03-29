package templates

import (
	"fmt"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func cloneTemplateDocument(payload bson.M) bson.M {
	template := bson.M{}
	for key, value := range payload {
		template[key] = value
	}
	return template
}

func normalizeTemplatePayload(payload bson.M, isCreate bool) (bson.M, error) {
	template := cloneTemplateDocument(payload)

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

func buildTemplateVerification(template bson.M) bson.M {
	issues := make([]bson.M, 0, 8)
	addIssue := func(code, level, message string) {
		issues = append(issues, bson.M{
			"code":    code,
			"level":   level,
			"message": message,
		})
	}

	templateType := strings.ToLower(strings.TrimSpace(shared.StringValue(template["type"])))
	templateKind := strings.ToLower(strings.TrimSpace(shared.StringValue(template["templateKind"])))
	repoMode := strings.ToLower(strings.TrimSpace(shared.StringValue(template["repoMode"])))
	if repoMode == "" {
		repoMode = "existing"
	}

	description := strings.TrimSpace(shared.StringValue(template["description"]))
	if description == "" {
		addIssue("missing-description", "warning", "Add a short description so users understand when to pick this template.")
	}
	category := strings.TrimSpace(shared.StringValue(template["category"]))
	if category == "" {
		addIssue("missing-category", "warning", "Add a category to keep the service catalog grouped consistently.")
	}
	owner := strings.TrimSpace(shared.StringValue(template["owner"]))
	if owner == "" {
		addIssue("missing-owner", "warning", "Set an owner so developers know who maintains this blueprint.")
	}
	bestFor := strings.TrimSpace(shared.StringValue(template["bestFor"]))
	if bestFor == "" {
		addIssue("missing-best-for", "warning", "Document the main use case in bestFor.")
	}
	setupTime := strings.TrimSpace(shared.StringValue(template["setupTime"]))
	if setupTime == "" {
		addIssue("missing-setup-time", "warning", "Add setupTime to help teams choose the fastest valid path.")
	}
	tier := strings.TrimSpace(shared.StringValue(template["tier"]))
	if tier == "" {
		addIssue("missing-tier", "warning", "Tag the template tier so the catalog can distinguish core and experimental paths.")
	}

	defaultsRaw, _ := normalizeMap(template["templateDefaults"])
	switch templateType {
	case "microservice":
		if templateKind == "scheduled-job" {
			if strings.TrimSpace(shared.StringValue(defaultsRaw["scheduleCron"])) == "" {
				addIssue("missing-schedule-cron", "warning", "Scheduled-job templates should provide a default cron expression.")
			}
			if strings.TrimSpace(shared.StringValue(defaultsRaw["scheduleCommand"])) == "" {
				addIssue("missing-schedule-command", "warning", "Scheduled-job templates should provide a default command.")
			}
		} else {
			if strings.TrimSpace(shared.StringValue(defaultsRaw["port"])) == "" {
				addIssue("missing-port", "warning", "Service templates should provide a default runtime port.")
			}
			if strings.TrimSpace(shared.StringValue(defaultsRaw["healthCheckPath"])) == "" {
				addIssue("missing-health-check", "warning", "Service templates should provide a default health check path.")
			}
		}
	case "static-site":
		if strings.TrimSpace(shared.StringValue(defaultsRaw["framework"])) == "" {
			addIssue("missing-framework", "warning", "Static-site templates should declare the starter framework.")
		}
		if strings.TrimSpace(shared.StringValue(defaultsRaw["buildCommand"])) == "" {
			addIssue("missing-build-command", "warning", "Static-site templates should provide a build command.")
		}
		if strings.TrimSpace(shared.StringValue(defaultsRaw["outputDir"])) == "" {
			addIssue("missing-output-dir", "warning", "Static-site templates should provide the output directory.")
		}
	}

	sourceRaw, hasSource := normalizeMap(template["templateSource"])
	if repoMode == "template" {
		if !hasSource {
			addIssue("missing-template-source", "error", "Template repo mode requires templateSource.")
		} else if strings.TrimSpace(shared.StringValue(sourceRaw["path"])) == "" {
			addIssue("missing-template-source-path", "warning", "Add templateSource.path to target a stable blueprint folder.")
		}
	}

	status := "verified"
	summary := "Template passed verification with production-ready defaults."
	if len(issues) > 0 {
		status = "needs-review"
		summary = "Template passed validation but still needs stronger defaults or metadata."
		for _, issue := range issues {
			if shared.StringValue(issue["level"]) == "error" {
				status = "invalid"
				summary = "Template failed verification and should be fixed before use."
				break
			}
		}
	}

	return bson.M{
		"verified": len(issues) == 0,
		"status":   status,
		"summary":  summary,
		"issues":   issues,
	}
}

func enrichTemplateDocument(template bson.M) bson.M {
	enriched := cloneTemplateDocument(template)
	enriched["verification"] = buildTemplateVerification(enriched)
	return enriched
}

func enrichTemplateDocuments(items []bson.M) []bson.M {
	if len(items) == 0 {
		return items
	}
	out := make([]bson.M, 0, len(items))
	for _, item := range items {
		out = append(out, enrichTemplateDocument(item))
	}
	return out
}

func verifyTemplateCandidate(payload bson.M) bson.M {
	normalized, err := normalizeTemplatePayload(payload, false)
	if err != nil {
		template := cloneTemplateDocument(payload)
		template["verification"] = bson.M{
			"verified": false,
			"status":   "invalid",
			"summary":  "Template payload is invalid and cannot be imported yet.",
			"issues": []bson.M{
				{
					"code":    "invalid-template-payload",
					"level":   "error",
					"message": err.Error(),
				},
			},
		}
		return template
	}
	return enrichTemplateDocument(normalized)
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
