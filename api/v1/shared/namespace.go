package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Fixed application namespaces. All app workloads MUST deploy into one of these.
const (
	NamespaceProduction  = "releasea-apps-production"
	NamespaceStaging     = "releasea-apps-staging"
	NamespaceDevelopment = "releasea-apps-development"
	NamespaceSystem      = "releasea-system"
)

// defaultNamespaceMapping maps environment names to fixed namespaces.
// "production-like" → releasea-apps-production
// "staging-like"    → releasea-apps-staging
// everything else   → releasea-apps-development
var defaultNamespaceMapping = map[string]string{
	"prod":       NamespaceProduction,
	"production": NamespaceProduction,
	"live":       NamespaceProduction,

	"staging":     NamespaceStaging,
	"stage":       NamespaceStaging,
	"pre-prod":    NamespaceStaging,
	"preprod":     NamespaceStaging,
	"uat":         NamespaceStaging,
	"pre-release": NamespaceStaging,

	"dev":         NamespaceDevelopment,
	"development": NamespaceDevelopment,
	"qa":          NamespaceDevelopment,
	"sandbox":     NamespaceDevelopment,
	"test":        NamespaceDevelopment,
	"testing":     NamespaceDevelopment,
	"preview":     NamespaceDevelopment,
	"feature":     NamespaceDevelopment,
	"ci":          NamespaceDevelopment,
	"local":       NamespaceDevelopment,
}

// LoadNamespaceMapping returns the mapping, merging any overrides from
// the RELEASEA_NAMESPACE_MAPPING env var (JSON object: {"env": "namespace"}).
func LoadNamespaceMapping() map[string]string {
	merged := make(map[string]string, len(defaultNamespaceMapping))
	for k, v := range defaultNamespaceMapping {
		merged[k] = v
	}

	raw := strings.TrimSpace(os.Getenv("RELEASEA_NAMESPACE_MAPPING"))
	if raw == "" {
		return merged
	}

	var overrides map[string]string
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		return merged
	}
	for env, ns := range overrides {
		env = strings.TrimSpace(strings.ToLower(env))
		ns = strings.TrimSpace(ns)
		if env != "" && ns != "" && IsValidAppNamespace(ns) {
			merged[env] = ns
		}
	}
	return merged
}

// ResolveAppNamespace maps an environment name to one of the three fixed
// application namespaces. This is the SINGLE SOURCE OF TRUTH for namespace
// resolution across the entire platform.
//
// Rules:
//  1. Empty environment defaults to "prod" → releasea-apps-production.
//  2. Known environment names are looked up in the mapping.
//  3. Unknown environments fall back to releasea-apps-development.
//  4. The result is NEVER releasea-system.
func ResolveAppNamespace(environment string) string {
	environment = strings.TrimSpace(strings.ToLower(environment))
	if environment == "" {
		environment = "prod"
	}

	mapping := LoadNamespaceMapping()
	if ns, ok := mapping[environment]; ok {
		return ns
	}

	return NamespaceDevelopment
}

// IsValidAppNamespace returns true only for the three allowed app namespaces.
func IsValidAppNamespace(namespace string) bool {
	switch namespace {
	case NamespaceProduction, NamespaceStaging, NamespaceDevelopment:
		return true
	default:
		return false
	}
}

// IsSystemNamespace returns true if the namespace is the reserved system namespace.
func IsSystemNamespace(namespace string) bool {
	return strings.TrimSpace(strings.ToLower(namespace)) == NamespaceSystem
}

// ValidateAppNamespace checks that a computed namespace is safe for app workloads.
// Returns an error if the namespace is releasea-system or otherwise invalid.
func ValidateAppNamespace(namespace string) error {
	if IsSystemNamespace(namespace) {
		return fmt.Errorf("namespace %q is reserved for platform components; application workloads cannot be deployed there", namespace)
	}
	if !IsValidAppNamespace(namespace) {
		return fmt.Errorf("namespace %q is not a valid application namespace; allowed: %s, %s, %s",
			namespace, NamespaceProduction, NamespaceStaging, NamespaceDevelopment)
	}
	return nil
}

// EnvironmentsShareNamespace returns true when two environment names resolve
// to the same fixed namespace.
func EnvironmentsShareNamespace(envA, envB string) bool {
	return ResolveAppNamespace(envA) == ResolveAppNamespace(envB)
}
