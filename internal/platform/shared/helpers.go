package shared

import (
	platformutils "releaseaapi/internal/platform/utils"

	"github.com/gin-gonic/gin"
)

func StringValue(value interface{}) string {
	return platformutils.StringValue(value)
}

func IntValue(value interface{}) int {
	return platformutils.IntValue(value)
}

func BoolValue(value interface{}) bool {
	return platformutils.BoolValue(value)
}

func ToStringSlice(value interface{}) []string {
	return platformutils.ToStringSlice(value)
}

func ToInterfaceSlice(value interface{}) []interface{} {
	return platformutils.ToInterfaceSlice(value)
}

func MapPayload(value interface{}) map[string]interface{} {
	return platformutils.MapPayload(value)
}

func UniqueStrings(values []string) []string {
	return platformutils.UniqueStrings(values)
}

func EnvOrDefault(key, fallback string) string {
	return platformutils.EnvOrDefault(key, fallback)
}

func EnvBool(key string, fallback bool) bool {
	return platformutils.EnvBool(key, fallback)
}

func TokenHint(token string) string {
	return platformutils.TokenHint(token)
}

func ToKubeName(value string) string {
	return platformutils.ToKubeName(value)
}

func AuthDisplayName(c *gin.Context) string {
	return platformutils.AuthDisplayName(c)
}

const DefaultRuleName = platformutils.DefaultRuleName

func LegacyDefaultRuleName(serviceName string) string {
	return platformutils.LegacyDefaultRuleName(serviceName)
}

func CanonicalRuleName(ruleName, serviceName string) string {
	return platformutils.CanonicalRuleName(ruleName, serviceName)
}
