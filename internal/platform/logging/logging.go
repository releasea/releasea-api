package logging

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

type LogFields map[string]interface{}

func LogInfo(event string, fields LogFields) {
	logStructured("info", event, nil, fields)
}

func LogWarn(event string, fields LogFields) {
	logStructured("warn", event, nil, fields)
}

func LogError(event string, err error, fields LogFields) {
	logStructured("error", event, err, fields)
}

func logStructured(level string, event string, err error, fields LogFields) {
	record := map[string]interface{}{
		"level": strings.ToLower(strings.TrimSpace(level)),
		"event": strings.TrimSpace(event),
		"ts":    nowISO(),
	}
	for key, value := range fields {
		record[key] = value
	}
	if err != nil {
		record["error"] = err.Error()
	}

	encoded, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		log.Printf("level=%s event=%s log_marshal_error=%v", record["level"], record["event"], marshalErr)
		return
	}
	log.Printf("%s", encoded)
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
