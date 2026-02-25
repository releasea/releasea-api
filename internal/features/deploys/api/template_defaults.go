package deploys

import "strings"

func DefaultDeployTemplateResources(templateID, templateType string) []interface{} {
	key := strings.ToLower(strings.TrimSpace(templateID))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(templateType))
	}
	switch key {
	case "tpl-git", "git":
		return defaultServiceResources()
	case "tpl-registry", "registry", "docker":
		return defaultServiceResources()
	case "tpl-cronjob", "cronjob":
		return defaultCronJobResources()
	default:
		return nil
	}
}

func defaultServiceResources() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "{{serviceName}}",
				"namespace": "{{namespace}}",
				"labels": map[string]interface{}{
					"app": "{{serviceName}}",
				},
			},
			"spec": map[string]interface{}{
				"replicas": 1,
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "{{serviceName}}",
					},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app": "{{serviceName}}",
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "{{serviceName}}",
								"image": "{{image}}",
								"ports": []interface{}{
									map[string]interface{}{
										"containerPort": "{{port}}",
									},
								},
							},
						},
					},
				},
			},
		},
		map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "{{serviceName}}",
				"namespace": "{{namespace}}",
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"app": "{{serviceName}}",
				},
				"ports": []interface{}{
					map[string]interface{}{
						"name":       "http",
						"port":       "{{port}}",
						"targetPort": "{{port}}",
					},
				},
			},
		},
	}
}

func defaultCronJobResources() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]interface{}{
				"name":      "{{serviceName}}",
				"namespace": "{{namespace}}",
				"labels": map[string]interface{}{
					"app": "{{serviceName}}",
				},
			},
			"spec": map[string]interface{}{
				"schedule":                   "{{scheduleCron}}",
				"timeZone":                   "{{scheduleTimezone}}",
				"concurrencyPolicy":          "Forbid",
				"successfulJobsHistoryLimit": 3,
				"failedJobsHistoryLimit":     3,
				"jobTemplate": map[string]interface{}{
					"spec": map[string]interface{}{
						"backoffLimit":          "{{scheduleRetries}}",
						"activeDeadlineSeconds": "{{scheduleTimeout}}",
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"labels": map[string]interface{}{
									"app": "{{serviceName}}",
								},
							},
							"spec": map[string]interface{}{
								"restartPolicy": "Never",
								"containers": []interface{}{
									map[string]interface{}{
										"name":    "{{serviceName}}",
										"image":   "{{image}}",
										"command": []interface{}{"sh", "-c", "{{scheduleCommand}}"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
