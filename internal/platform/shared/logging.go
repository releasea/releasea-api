package shared

import platformlogging "releaseaapi/internal/platform/logging"

type LogFields = platformlogging.LogFields

func LogInfo(event string, fields LogFields) {
	platformlogging.LogInfo(event, fields)
}

func LogWarn(event string, fields LogFields) {
	platformlogging.LogWarn(event, fields)
}

func LogError(event string, err error, fields LogFields) {
	platformlogging.LogError(event, err, fields)
}
