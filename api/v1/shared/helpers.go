package shared

import (
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func StringValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func IntValue(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func BoolValue(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

func ToStringSlice(value interface{}) []string {
	if value == nil {
		return []string{}
	}
	switch v := value.(type) {
	case []string:
		return append([]string{}, v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{}
	}
}

func MapPayload(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	if payload, ok := value.(bson.M); ok {
		return map[string]interface{}(payload)
	}
	if payload, ok := value.(map[string]interface{}); ok {
		return payload
	}
	return map[string]interface{}{}
}

func UniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func EnvOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func EnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "true", "1", "yes", "y":
		return true
	case "false", "0", "no", "n":
		return false
	default:
		return fallback
	}
}

func TokenHint(token string) string {
	if len(token) <= 6 {
		return token
	}
	return token[len(token)-6:]
}

func ToKubeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return value
}

func AuthDisplayName(c *gin.Context) string {
	if value, ok := c.Get("authName"); ok {
		if name, ok := value.(string); ok && name != "" {
			return name
		}
	}
	if value, ok := c.Get("authEmail"); ok {
		if email, ok := value.(string); ok && email != "" {
			return email
		}
	}
	return ""
}

const DefaultRuleName = "rule-default"

func LegacyDefaultRuleName(serviceName string) string {
	trimmedServiceName := strings.TrimSpace(serviceName)
	if trimmedServiceName == "" {
		return ""
	}
	return trimmedServiceName + "-default"
}

func CanonicalRuleName(ruleName, serviceName string) string {
	trimmedRuleName := strings.TrimSpace(ruleName)
	if trimmedRuleName == "" {
		return ""
	}
	if trimmedRuleName == DefaultRuleName {
		return DefaultRuleName
	}
	legacyDefault := LegacyDefaultRuleName(serviceName)
	if legacyDefault != "" && trimmedRuleName == legacyDefault {
		return DefaultRuleName
	}
	return trimmedRuleName
}
