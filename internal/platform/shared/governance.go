package shared

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	GovernanceApprovalTypeDeploy      = "deploy"
	GovernanceApprovalTypeRulePublish = "rule-publish"

	GovernanceExceptionPolicyDeploy = "deploy-policy"

	GovernanceApprovalStatusPending  = "pending"
	GovernanceApprovalStatusApproved = "approved"
	GovernanceApprovalStatusRejected = "rejected"
)

var governanceApprovalTypes = map[string]struct{}{
	GovernanceApprovalTypeDeploy:      {},
	GovernanceApprovalTypeRulePublish: {},
}

func NormalizeGovernanceApprovalType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if _, ok := governanceApprovalTypes[normalized]; ok {
		return normalized
	}
	return ""
}

func NormalizeGovernanceApprovalStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GovernanceApprovalStatusApproved:
		return GovernanceApprovalStatusApproved
	case GovernanceApprovalStatusRejected:
		return GovernanceApprovalStatusRejected
	case GovernanceApprovalStatusPending:
		return GovernanceApprovalStatusPending
	default:
		return ""
	}
}

func LoadGovernanceSettings(ctx context.Context) (bson.M, error) {
	settings, err := FindOne(ctx, Collection(GovernanceSettingsCollection), bson.M{})
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return bson.M{
			"deployApproval": bson.M{
				"enabled":      false,
				"environments": []string{"prod"},
				"minApprovers": 1,
			},
			"deployPolicy": bson.M{
				"enabled": false,
				"dryRun":  false,
				"rules":   []interface{}{},
			},
			"rulePublishApproval": bson.M{
				"enabled":      false,
				"externalOnly": false,
				"minApprovers": 1,
			},
			"auditRetentionDays": 30,
		}, nil
	}
	return nil, err
}

type GovernanceDeployPolicyViolation struct {
	Code        string                 `json:"code"`
	Environment string                 `json:"environment"`
	Message     string                 `json:"message"`
	Rule        map[string]interface{} `json:"rule,omitempty"`
}

type GovernanceDeployPolicyTarget struct {
	ProfileID        string `json:"profileId,omitempty"`
	SCMProvider      string `json:"scmProvider,omitempty"`
	RegistryProvider string `json:"registryProvider,omitempty"`
	SecretProvider   string `json:"secretProvider,omitempty"`
}

type GovernancePolicyExceptionSummary struct {
	ID          string   `json:"id"`
	Policy      string   `json:"policy"`
	Environment string   `json:"environment"`
	Codes       []string `json:"codes"`
	Reason      string   `json:"reason"`
	ExpiresAt   string   `json:"expiresAt"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	Status      string   `json:"status,omitempty"`
}

func normalizeDeployStrategy(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "rolling", "canary", "blue-green":
		return normalized
	default:
		return ""
	}
}

func normalizeDeploySourceType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "git", "registry":
		return normalized
	default:
		return ""
	}
}

func normalizePolicyIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeGovernanceExceptionPolicy(value string) string {
	switch normalizePolicyIdentifier(value) {
	case "", "deploy", GovernanceExceptionPolicyDeploy:
		return GovernanceExceptionPolicyDeploy
	default:
		return ""
	}
}

func NormalizeGovernanceExceptionCodes(values []string) []string {
	if len(values) == 0 {
		return []string{"*"}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizePolicyIdentifier(value)
		if normalized == "" {
			continue
		}
		if normalized == "all" {
			normalized = "*"
		}
		if normalized == "*" {
			return []string{"*"}
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return []string{"*"}
	}
	sort.Strings(result)
	return result
}

func NormalizeRegistryHost(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil {
			value = parsed.Host
		}
	}
	value = strings.TrimSuffix(value, "/v1/")
	value = strings.TrimSuffix(value, "/")
	if strings.Contains(value, "/") {
		return ""
	}
	if value == "index.docker.io" {
		return "docker.io"
	}
	return value
}

func NormalizeDeployPolicyRules(raw interface{}) []bson.M {
	items := ToInterfaceSlice(raw)
	if len(items) == 0 {
		return []bson.M{}
	}

	normalizedByEnvironment := map[string]bson.M{}
	order := make([]string, 0, len(items))
	for _, item := range items {
		rule := MapPayload(item)
		environment := NormalizeOperationEnvironment(StringValue(rule["environment"]))
		if environment == "" {
			continue
		}

		seenStrategies := map[string]struct{}{}
		allowedStrategies := make([]string, 0)
		for _, strategy := range ToStringSlice(rule["allowedStrategies"]) {
			normalizedStrategy := normalizeDeployStrategy(strategy)
			if normalizedStrategy == "" {
				continue
			}
			if _, exists := seenStrategies[normalizedStrategy]; exists {
				continue
			}
			seenStrategies[normalizedStrategy] = struct{}{}
			allowedStrategies = append(allowedStrategies, normalizedStrategy)
		}
		sort.Strings(allowedStrategies)

		seenSourceTypes := map[string]struct{}{}
		allowedSourceTypes := make([]string, 0)
		for _, sourceType := range ToStringSlice(rule["allowedSourceTypes"]) {
			normalizedSourceType := normalizeDeploySourceType(sourceType)
			if normalizedSourceType == "" {
				continue
			}
			if _, exists := seenSourceTypes[normalizedSourceType]; exists {
				continue
			}
			seenSourceTypes[normalizedSourceType] = struct{}{}
			allowedSourceTypes = append(allowedSourceTypes, normalizedSourceType)
		}
		sort.Strings(allowedSourceTypes)

		allowedProfileIDs := normalizePolicyIdentifierList(ToStringSlice(rule["allowedProfileIds"]))
		allowedSCMProviders := normalizePolicyIdentifierList(ToStringSlice(rule["allowedScmProviders"]))
		allowedRegistryProviders := normalizePolicyIdentifierList(ToStringSlice(rule["allowedRegistryProviders"]))
		allowedSecretProviders := normalizePolicyIdentifierList(ToStringSlice(rule["allowedSecretProviders"]))

		seenRegistries := map[string]struct{}{}
		allowedRegistries := make([]string, 0)
		for _, registryHost := range ToStringSlice(rule["allowedRegistries"]) {
			normalizedRegistryHost := NormalizeRegistryHost(registryHost)
			if normalizedRegistryHost == "" {
				continue
			}
			if _, exists := seenRegistries[normalizedRegistryHost]; exists {
				continue
			}
			seenRegistries[normalizedRegistryHost] = struct{}{}
			allowedRegistries = append(allowedRegistries, normalizedRegistryHost)
		}
		sort.Strings(allowedRegistries)

		maxReplicas := IntValue(rule["maxReplicas"])
		if maxReplicas < 0 {
			maxReplicas = 0
		}

		normalizedByEnvironment[environment] = bson.M{
			"environment":              environment,
			"allowAutoDeploy":          BoolValue(rule["allowAutoDeploy"]),
			"requireExplicitVersion":   BoolValue(rule["requireExplicitVersion"]),
			"blockExternalExposure":    BoolValue(rule["blockExternalExposure"]),
			"allowedProfileIds":        allowedProfileIDs,
			"allowedScmProviders":      allowedSCMProviders,
			"allowedRegistryProviders": allowedRegistryProviders,
			"allowedSecretProviders":   allowedSecretProviders,
			"allowedSourceTypes":       allowedSourceTypes,
			"allowedRegistries":        allowedRegistries,
			"allowedStrategies":        allowedStrategies,
			"maxReplicas":              maxReplicas,
		}
		if !containsString(order, environment) {
			order = append(order, environment)
		}
	}

	sort.Strings(order)
	result := make([]bson.M, 0, len(order))
	for _, environment := range order {
		if rule, ok := normalizedByEnvironment[environment]; ok {
			result = append(result, rule)
		}
	}
	return result
}

func IsDeployPolicyDryRun(settings bson.M) bool {
	config := MapPayload(settings["deployPolicy"])
	return BoolValue(config["dryRun"])
}

func GovernanceTemporaryExceptionStatus(item bson.M, now time.Time) string {
	if strings.TrimSpace(StringValue(item["revokedAt"])) != "" {
		return "revoked"
	}
	expiresAt := strings.TrimSpace(StringValue(item["expiresAt"]))
	if expiresAt == "" {
		return "expired"
	}
	parsed, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return "expired"
	}
	if parsed.After(now.UTC()) {
		return "active"
	}
	return "expired"
}

func IsGovernanceTemporaryExceptionActive(item bson.M, now time.Time) bool {
	return GovernanceTemporaryExceptionStatus(item, now) == "active"
}

func BuildGovernancePolicyExceptionSummary(item bson.M, now time.Time) GovernancePolicyExceptionSummary {
	return GovernancePolicyExceptionSummary{
		ID:          strings.TrimSpace(StringValue(item["id"])),
		Policy:      NormalizeGovernanceExceptionPolicy(StringValue(item["policy"])),
		Environment: NormalizeOperationEnvironment(StringValue(item["environment"])),
		Codes:       NormalizeGovernanceExceptionCodes(ToStringSlice(item["codes"])),
		Reason:      strings.TrimSpace(StringValue(item["reason"])),
		ExpiresAt:   strings.TrimSpace(StringValue(item["expiresAt"])),
		CreatedAt:   strings.TrimSpace(StringValue(item["createdAt"])),
		Status:      GovernanceTemporaryExceptionStatus(item, now),
	}
}

func FilterDeployPolicyViolationsWithExceptions(
	violations []GovernanceDeployPolicyViolation,
	exceptions []bson.M,
) ([]GovernanceDeployPolicyViolation, []GovernancePolicyExceptionSummary) {
	if len(violations) == 0 || len(exceptions) == 0 {
		return violations, []GovernancePolicyExceptionSummary{}
	}

	now := time.Now().UTC()
	appliedByID := map[string]GovernancePolicyExceptionSummary{}
	remaining := make([]GovernanceDeployPolicyViolation, 0, len(violations))

	for _, violation := range violations {
		matched := false
		for _, exception := range exceptions {
			if !IsGovernanceTemporaryExceptionActive(exception, now) {
				continue
			}
			if NormalizeGovernanceExceptionPolicy(StringValue(exception["policy"])) != GovernanceExceptionPolicyDeploy {
				continue
			}
			if environment := NormalizeOperationEnvironment(StringValue(exception["environment"])); environment != "" && environment != NormalizeOperationEnvironment(violation.Environment) {
				continue
			}
			if !governanceExceptionCodeMatches(ToStringSlice(exception["codes"]), violation.Code) {
				continue
			}

			summary := BuildGovernancePolicyExceptionSummary(exception, now)
			if summary.ID != "" {
				appliedByID[summary.ID] = summary
			}
			matched = true
			break
		}
		if !matched {
			remaining = append(remaining, violation)
		}
	}

	applied := make([]GovernancePolicyExceptionSummary, 0, len(appliedByID))
	for _, summary := range appliedByID {
		applied = append(applied, summary)
	}
	sort.Slice(applied, func(i, j int) bool {
		if applied[i].ExpiresAt == applied[j].ExpiresAt {
			return applied[i].ID < applied[j].ID
		}
		return applied[i].ExpiresAt < applied[j].ExpiresAt
	})

	return remaining, applied
}

func governanceExceptionCodeMatches(codes []string, code string) bool {
	normalizedCode := normalizePolicyIdentifier(code)
	if normalizedCode == "" {
		return false
	}
	normalizedCodes := NormalizeGovernanceExceptionCodes(codes)
	if len(normalizedCodes) == 0 {
		return true
	}
	for _, candidate := range normalizedCodes {
		if candidate == "*" || candidate == normalizedCode {
			return true
		}
	}
	return false
}

func normalizePolicyIdentifierList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizePolicyIdentifier(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func EvaluateDeployPolicy(
	settings bson.M,
	environment,
	trigger,
	strategyType,
	sourceType,
	registryHost string,
	replicas int,
	explicitVersion bool,
	target GovernanceDeployPolicyTarget,
) []GovernanceDeployPolicyViolation {
	config := MapPayload(settings["deployPolicy"])
	if !BoolValue(config["enabled"]) {
		return nil
	}

	targetEnvironment := NormalizeOperationEnvironment(environment)
	if targetEnvironment == "" {
		return nil
	}
	normalizedTrigger := strings.ToLower(strings.TrimSpace(trigger))
	normalizedStrategy := normalizeDeployStrategy(strategyType)
	normalizedSourceType := normalizeDeploySourceType(sourceType)
	normalizedRegistryHost := NormalizeRegistryHost(registryHost)
	normalizedProfileID := normalizePolicyIdentifier(target.ProfileID)
	normalizedSCMProvider := normalizePolicyIdentifier(target.SCMProvider)
	normalizedRegistryProvider := normalizePolicyIdentifier(target.RegistryProvider)
	normalizedSecretProvider := normalizePolicyIdentifier(target.SecretProvider)
	if replicas < 0 {
		replicas = 0
	}

	violations := make([]GovernanceDeployPolicyViolation, 0)
	for _, item := range NormalizeDeployPolicyRules(config["rules"]) {
		ruleEnvironment := NormalizeOperationEnvironment(StringValue(item["environment"]))
		if ruleEnvironment == "" || ruleEnvironment != targetEnvironment {
			continue
		}

		allowAutoDeploy := BoolValue(item["allowAutoDeploy"])
		requireExplicitVersion := BoolValue(item["requireExplicitVersion"])
		allowedProfileIDs := ToStringSlice(item["allowedProfileIds"])
		allowedSCMProviders := ToStringSlice(item["allowedScmProviders"])
		allowedRegistryProviders := ToStringSlice(item["allowedRegistryProviders"])
		allowedSecretProviders := ToStringSlice(item["allowedSecretProviders"])
		allowedSourceTypes := ToStringSlice(item["allowedSourceTypes"])
		allowedRegistries := ToStringSlice(item["allowedRegistries"])
		allowedStrategies := ToStringSlice(item["allowedStrategies"])
		maxReplicas := IntValue(item["maxReplicas"])

		if normalizedTrigger == "auto" && !allowAutoDeploy {
			violations = append(violations, GovernanceDeployPolicyViolation{
				Code:        "auto-deploy-disabled",
				Environment: targetEnvironment,
				Message:     fmt.Sprintf("Auto deploy is disabled by policy for environment %s.", targetEnvironment),
				Rule:        item,
			})
		}
		if requireExplicitVersion && !explicitVersion {
			violations = append(violations, GovernanceDeployPolicyViolation{
				Code:        "explicit-version-required",
				Environment: targetEnvironment,
				Message:     fmt.Sprintf("An explicit version is required by policy for environment %s.", targetEnvironment),
				Rule:        item,
			})
		}
		if len(allowedProfileIDs) > 0 {
			if normalizedProfileID == "" {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "runtime-profile-unresolved",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("A runtime profile is required by policy for environment %s.", targetEnvironment),
					Rule:        item,
				})
			} else if !containsNormalizedValue(allowedProfileIDs, normalizedProfileID) {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "runtime-profile-not-allowed",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Runtime profile %s is not allowed by policy for environment %s.", normalizedProfileID, targetEnvironment),
					Rule:        item,
				})
			}
		}
		if len(allowedSCMProviders) > 0 && normalizedSourceType == "git" {
			if normalizedSCMProvider == "" {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "scm-provider-unresolved",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("SCM provider could not be resolved for environment %s, but policy requires an allowed SCM provider.", targetEnvironment),
					Rule:        item,
				})
			} else if !containsNormalizedValue(allowedSCMProviders, normalizedSCMProvider) {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "scm-provider-not-allowed",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("SCM provider %s is not allowed by policy for environment %s.", normalizedSCMProvider, targetEnvironment),
					Rule:        item,
				})
			}
		}
		if len(allowedRegistryProviders) > 0 && normalizedSourceType == "registry" {
			if normalizedRegistryProvider == "" {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "registry-provider-unresolved",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Registry provider could not be resolved for environment %s, but policy requires an allowed registry provider.", targetEnvironment),
					Rule:        item,
				})
			} else if !containsNormalizedValue(allowedRegistryProviders, normalizedRegistryProvider) {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "registry-provider-not-allowed",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Registry provider %s is not allowed by policy for environment %s.", normalizedRegistryProvider, targetEnvironment),
					Rule:        item,
				})
			}
		}
		if len(allowedSecretProviders) > 0 {
			if normalizedSecretProvider == "" {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "secret-provider-unresolved",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Secret provider could not be resolved for environment %s, but policy requires an allowed secret provider.", targetEnvironment),
					Rule:        item,
				})
			} else if !containsNormalizedValue(allowedSecretProviders, normalizedSecretProvider) {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "secret-provider-not-allowed",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Secret provider %s is not allowed by policy for environment %s.", normalizedSecretProvider, targetEnvironment),
					Rule:        item,
				})
			}
		}
		if len(allowedSourceTypes) > 0 && normalizedSourceType != "" {
			allowed := false
			for _, candidate := range allowedSourceTypes {
				if normalizeDeploySourceType(candidate) == normalizedSourceType {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "source-type-not-allowed",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Source type %s is not allowed by policy for environment %s.", normalizedSourceType, targetEnvironment),
					Rule:        item,
				})
			}
		}
		if len(allowedRegistries) > 0 {
			if normalizedRegistryHost == "" {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "registry-host-unresolved",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Registry host could not be resolved for environment %s, but policy requires an allowed registry.", targetEnvironment),
					Rule:        item,
				})
			} else {
				allowed := false
				for _, candidate := range allowedRegistries {
					if NormalizeRegistryHost(candidate) == normalizedRegistryHost {
						allowed = true
						break
					}
				}
				if !allowed {
					violations = append(violations, GovernanceDeployPolicyViolation{
						Code:        "registry-not-allowed",
						Environment: targetEnvironment,
						Message:     fmt.Sprintf("Registry %s is not allowed by policy for environment %s.", normalizedRegistryHost, targetEnvironment),
						Rule:        item,
					})
				}
			}
		}
		if len(allowedStrategies) > 0 && normalizedStrategy != "" {
			allowed := false
			for _, candidate := range allowedStrategies {
				if normalizeDeployStrategy(candidate) == normalizedStrategy {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, GovernanceDeployPolicyViolation{
					Code:        "strategy-not-allowed",
					Environment: targetEnvironment,
					Message:     fmt.Sprintf("Strategy %s is not allowed by policy for environment %s.", normalizedStrategy, targetEnvironment),
					Rule:        item,
				})
			}
		}
		if maxReplicas > 0 && replicas > maxReplicas {
			violations = append(violations, GovernanceDeployPolicyViolation{
				Code:        "max-replicas-exceeded",
				Environment: targetEnvironment,
				Message:     fmt.Sprintf("Requested deploy uses %d replicas, which exceeds the policy limit of %d for environment %s.", replicas, maxReplicas, targetEnvironment),
				Rule:        item,
			})
		}
	}
	return violations
}

func containsNormalizedValue(values []string, target string) bool {
	for _, candidate := range values {
		if normalizePolicyIdentifier(candidate) == target {
			return true
		}
	}
	return false
}

func EvaluateExternalExposurePolicy(settings bson.M, environment string, external bool) []GovernanceDeployPolicyViolation {
	config := MapPayload(settings["deployPolicy"])
	if !BoolValue(config["enabled"]) || !external {
		return nil
	}

	targetEnvironment := NormalizeOperationEnvironment(environment)
	if targetEnvironment == "" {
		return nil
	}

	violations := make([]GovernanceDeployPolicyViolation, 0)
	for _, item := range NormalizeDeployPolicyRules(config["rules"]) {
		ruleEnvironment := NormalizeOperationEnvironment(StringValue(item["environment"]))
		if ruleEnvironment == "" || ruleEnvironment != targetEnvironment {
			continue
		}

		if BoolValue(item["blockExternalExposure"]) {
			violations = append(violations, GovernanceDeployPolicyViolation{
				Code:        "external-exposure-disabled",
				Environment: targetEnvironment,
				Message:     fmt.Sprintf("External exposure is blocked by policy for environment %s.", targetEnvironment),
				Rule:        item,
			})
		}
	}
	return violations
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func DeployApprovalRequired(settings bson.M, environment string) (bool, int) {
	config := MapPayload(settings["deployApproval"])
	if !BoolValue(config["enabled"]) {
		return false, 1
	}
	minApprovers := IntValue(config["minApprovers"])
	if minApprovers < 1 {
		minApprovers = 1
	}

	targetEnvironment := NormalizeOperationEnvironment(environment)
	allowedEnvironments := ToStringSlice(config["environments"])
	if len(allowedEnvironments) == 0 {
		return targetEnvironment == "prod", minApprovers
	}
	for _, value := range allowedEnvironments {
		if NormalizeOperationEnvironment(value) == targetEnvironment {
			return true, minApprovers
		}
	}
	return false, minApprovers
}

func RulePublishApprovalRequired(settings bson.M, external bool) (bool, int) {
	config := MapPayload(settings["rulePublishApproval"])
	if !BoolValue(config["enabled"]) {
		return false, 1
	}
	if BoolValue(config["externalOnly"]) && !external {
		return false, 1
	}
	minApprovers := IntValue(config["minApprovers"])
	if minApprovers < 1 {
		minApprovers = 1
	}
	return true, minApprovers
}

func MinApproversForApprovalType(settings bson.M, approvalType string) int {
	normalizedType := NormalizeGovernanceApprovalType(approvalType)
	minApprovers := 1
	switch normalizedType {
	case GovernanceApprovalTypeDeploy:
		minApprovers = IntValue(MapPayload(settings["deployApproval"])["minApprovers"])
	case GovernanceApprovalTypeRulePublish:
		minApprovers = IntValue(MapPayload(settings["rulePublishApproval"])["minApprovers"])
	}
	if minApprovers < 1 {
		return 1
	}
	return minApprovers
}

type GovernanceApprovalCreateParams struct {
	Type              string
	ResourceID        string
	ResourceName      string
	Environment       string
	RequestedBy       bson.M
	Metadata          map[string]interface{}
	RequiredApprovers int
}

func CreateOrGetPendingGovernanceApproval(ctx context.Context, params GovernanceApprovalCreateParams) (bson.M, bool, error) {
	approvalType := NormalizeGovernanceApprovalType(params.Type)
	if approvalType == "" {
		return nil, false, errors.New("unsupported approval type")
	}
	resourceID := strings.TrimSpace(params.ResourceID)
	if resourceID == "" {
		return nil, false, errors.New("resource ID required")
	}

	resourceName := strings.TrimSpace(params.ResourceName)
	if resourceName == "" {
		resourceName = resourceID
	}
	environment := strings.TrimSpace(params.Environment)
	if environment != "" {
		environment = NormalizeOperationEnvironment(environment)
	}

	filter := bson.M{
		"type":       approvalType,
		"resourceId": resourceID,
		"status":     GovernanceApprovalStatusPending,
	}
	if environment != "" {
		filter["environment"] = environment
	}
	existing, err := FindOne(ctx, Collection(GovernanceApprovalsCollection), filter)
	if err == nil {
		return existing, true, nil
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}

	requiredApprovers := params.RequiredApprovers
	if requiredApprovers < 1 {
		requiredApprovers = 1
	}

	requestedBy := bson.M{
		"id":    strings.TrimSpace(StringValue(params.RequestedBy["id"])),
		"name":  strings.TrimSpace(StringValue(params.RequestedBy["name"])),
		"email": strings.TrimSpace(StringValue(params.RequestedBy["email"])),
	}
	if StringValue(requestedBy["id"]) == "" {
		if name := StringValue(requestedBy["name"]); name != "" {
			requestedBy["id"] = ToKubeName(name)
		} else {
			requestedBy["id"] = "system"
		}
	}
	if StringValue(requestedBy["name"]) == "" {
		requestedBy["name"] = "System"
	}

	now := NowISO()
	approvalID := "apr-" + uuid.NewString()
	doc := bson.M{
		"_id":               approvalID,
		"id":                approvalID,
		"type":              approvalType,
		"status":            GovernanceApprovalStatusPending,
		"resourceId":        resourceID,
		"resourceName":      resourceName,
		"requestedBy":       requestedBy,
		"requestedAt":       now,
		"updatedAt":         now,
		"requiredApprovers": requiredApprovers,
		"approvalsCount":    0,
		"rejectionsCount":   0,
		"reviews":           []interface{}{},
	}
	if environment != "" {
		doc["environment"] = environment
	}
	if len(params.Metadata) > 0 {
		doc["metadata"] = params.Metadata
	}

	if err := InsertOne(ctx, Collection(GovernanceApprovalsCollection), doc); err != nil {
		return nil, false, err
	}
	return doc, false, nil
}
