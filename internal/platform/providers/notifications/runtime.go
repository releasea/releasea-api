package notificationproviders

import "strings"

type platformEventsRuntime struct{}

func (platformEventsRuntime) ID() string { return "platform-events" }

func (platformEventsRuntime) ValidateConfiguration(config map[string]interface{}) error {
	_ = config
	return nil
}

func (platformEventsRuntime) EnabledResourceCount(config map[string]interface{}) int {
	enabled := 0
	for _, key := range []string{"deploySuccess", "deployFailed", "serviceDown", "workerOffline", "highCpu", "approvalRequired", "approvalCompleted"} {
		if boolValue(config[key]) {
			enabled++
		}
	}
	return enabled
}

func boolValue(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
