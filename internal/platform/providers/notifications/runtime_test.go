package notificationproviders

import "testing"

func TestPlatformEventsRuntimeEnabledResourceCount(t *testing.T) {
	runtime := platformEventsRuntime{}
	config := map[string]interface{}{
		"deploySuccess":    true,
		"deployFailed":     false,
		"workerOffline":    "true",
		"approvalRequired": true,
	}

	if got := runtime.EnabledResourceCount(config); got != 3 {
		t.Fatalf("enabled notification count = %d, want %d", got, 3)
	}
}
