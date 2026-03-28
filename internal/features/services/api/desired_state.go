package services

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/yaml.v3"
)

var findServiceForDesiredState = func(ctx context.Context, serviceID string) (bson.M, error) {
	return shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
}

var findRulesForDesiredState = func(ctx context.Context, serviceID string) ([]bson.M, error) {
	return shared.FindAll(ctx, shared.Collection(shared.RulesCollection), bson.M{"serviceId": serviceID})
}

var nowForDesiredState = func() string {
	return shared.NowISO()
}

type serviceDesiredStateExport struct {
	Document serviceDesiredStateDocument `json:"document"`
	YAML     string                      `json:"yaml"`
	Filename string                      `json:"filename"`
	Warnings []string                    `json:"warnings,omitempty"`
}

type serviceDesiredStateDocument struct {
	Kind       string                     `json:"kind" yaml:"kind"`
	APIVersion string                     `json:"apiVersion" yaml:"apiVersion"`
	Version    int                        `json:"version" yaml:"version"`
	ExportedAt string                     `json:"exportedAt" yaml:"exportedAt"`
	Service    serviceDesiredStateService `json:"service" yaml:"service"`
	Rules      []serviceDesiredStateRule  `json:"rules" yaml:"rules"`
}

type serviceDesiredStateService struct {
	ID        string                         `json:"id" yaml:"id"`
	Name      string                         `json:"name" yaml:"name"`
	ProjectID string                         `json:"projectId" yaml:"projectId"`
	Type      string                         `json:"type" yaml:"type"`
	Spec      serviceDesiredStateServiceSpec `json:"spec" yaml:"spec"`
}

type serviceDesiredStateServiceSpec struct {
	ManagementMode string                             `json:"managementMode" yaml:"managementMode"`
	SourceType     string                             `json:"sourceType" yaml:"sourceType"`
	DeployTemplate string                             `json:"deployTemplateId,omitempty" yaml:"deployTemplateId,omitempty"`
	Repo           *serviceDesiredStateRepoSpec       `json:"repo,omitempty" yaml:"repo,omitempty"`
	Image          *serviceDesiredStateImageSpec      `json:"image,omitempty" yaml:"image,omitempty"`
	StaticSite     *serviceDesiredStateStaticSiteSpec `json:"staticSite,omitempty" yaml:"staticSite,omitempty"`
	Schedule       *serviceDesiredStateScheduleSpec   `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	Runtime        serviceDesiredStateRuntimeSpec     `json:"runtime" yaml:"runtime"`
	Credentials    serviceDesiredStateCredentialRefs  `json:"credentials" yaml:"credentials"`
	Environment    serviceDesiredStateEnvironmentSpec `json:"environment" yaml:"environment"`
	Features       serviceDesiredStateFeatureSpec     `json:"features" yaml:"features"`
}

type serviceDesiredStateRepoSpec struct {
	URL     string `json:"url,omitempty" yaml:"url,omitempty"`
	Branch  string `json:"branch,omitempty" yaml:"branch,omitempty"`
	RootDir string `json:"rootDir,omitempty" yaml:"rootDir,omitempty"`
}

type serviceDesiredStateImageSpec struct {
	DockerImage      string `json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	DockerContext    string `json:"dockerContext,omitempty" yaml:"dockerContext,omitempty"`
	DockerfilePath   string `json:"dockerfilePath,omitempty" yaml:"dockerfilePath,omitempty"`
	DockerCommand    string `json:"dockerCommand,omitempty" yaml:"dockerCommand,omitempty"`
	PreDeployCommand string `json:"preDeployCommand,omitempty" yaml:"preDeployCommand,omitempty"`
}

type serviceDesiredStateStaticSiteSpec struct {
	Framework      string      `json:"framework,omitempty" yaml:"framework,omitempty"`
	InstallCommand string      `json:"installCommand,omitempty" yaml:"installCommand,omitempty"`
	BuildCommand   string      `json:"buildCommand,omitempty" yaml:"buildCommand,omitempty"`
	OutputDir      string      `json:"outputDir,omitempty" yaml:"outputDir,omitempty"`
	CacheTTL       interface{} `json:"cacheTtl,omitempty" yaml:"cacheTtl,omitempty"`
}

type serviceDesiredStateScheduleSpec struct {
	Cron     string `json:"cron,omitempty" yaml:"cron,omitempty"`
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`
	Command  string `json:"command,omitempty" yaml:"command,omitempty"`
	Retries  string `json:"retries,omitempty" yaml:"retries,omitempty"`
	Timeout  string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

type serviceDesiredStateRuntimeSpec struct {
	Port               int                    `json:"port,omitempty" yaml:"port,omitempty"`
	HealthCheckPath    string                 `json:"healthCheckPath,omitempty" yaml:"healthCheckPath,omitempty"`
	Replicas           int                    `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	MinReplicas        int                    `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	MaxReplicas        int                    `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	CPU                int                    `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory             int                    `json:"memory,omitempty" yaml:"memory,omitempty"`
	ProfileID          string                 `json:"profileId,omitempty" yaml:"profileId,omitempty"`
	WorkerTags         []string               `json:"workerTags,omitempty" yaml:"workerTags,omitempty"`
	DeploymentStrategy map[string]interface{} `json:"deploymentStrategy,omitempty" yaml:"deploymentStrategy,omitempty"`
}

type serviceDesiredStateCredentialRefs struct {
	SCMCredentialID      string `json:"scmCredentialId,omitempty" yaml:"scmCredentialId,omitempty"`
	RegistryCredentialID string `json:"registryCredentialId,omitempty" yaml:"registryCredentialId,omitempty"`
	SecretProviderID     string `json:"secretProviderId,omitempty" yaml:"secretProviderId,omitempty"`
}

type serviceDesiredStateEnvironmentSpec struct {
	Keys           []string `json:"keys" yaml:"keys"`
	ValuesExcluded bool     `json:"valuesExcluded" yaml:"valuesExcluded"`
}

type serviceDesiredStateFeatureSpec struct {
	AutoDeploy              bool `json:"autoDeploy" yaml:"autoDeploy"`
	IsActive                bool `json:"isActive" yaml:"isActive"`
	PauseOnIdle             bool `json:"pauseOnIdle" yaml:"pauseOnIdle"`
	PauseIdleTimeoutSeconds int  `json:"pauseIdleTimeoutSeconds,omitempty" yaml:"pauseIdleTimeoutSeconds,omitempty"`
	RepoManaged             bool `json:"repoManaged" yaml:"repoManaged"`
}

type serviceDesiredStateRule struct {
	ID              string                             `json:"id" yaml:"id"`
	Name            string                             `json:"name" yaml:"name"`
	Environment     string                             `json:"environment" yaml:"environment"`
	Protocol        string                             `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Port            int                                `json:"port,omitempty" yaml:"port,omitempty"`
	Hosts           []string                           `json:"hosts,omitempty" yaml:"hosts,omitempty"`
	Paths           []string                           `json:"paths,omitempty" yaml:"paths,omitempty"`
	Methods         []string                           `json:"methods,omitempty" yaml:"methods,omitempty"`
	Gateways        []string                           `json:"gateways,omitempty" yaml:"gateways,omitempty"`
	Publication     serviceDesiredStatePublicationSpec `json:"publication" yaml:"publication"`
	Status          string                             `json:"status,omitempty" yaml:"status,omitempty"`
	Policy          map[string]interface{}             `json:"policy,omitempty" yaml:"policy,omitempty"`
	LastPublishedAt string                             `json:"lastPublishedAt,omitempty" yaml:"lastPublishedAt,omitempty"`
}

type serviceDesiredStatePublicationSpec struct {
	Internal bool `json:"internal" yaml:"internal"`
	External bool `json:"external" yaml:"external"`
}

func GetServiceDesiredState(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := findServiceForDesiredState(ctx, serviceID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Service not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service")
		return
	}
	if isObservedService(service) {
		c.JSON(http.StatusConflict, gin.H{
			"message": "Desired state export is only available for managed services.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}

	rules, err := findRulesForDesiredState(ctx, serviceID)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service rules")
		return
	}

	document, warnings := buildServiceDesiredStateDocument(service, rules, nowForDesiredState())
	rendered, err := yaml.Marshal(document)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to render desired state")
		return
	}

	c.JSON(http.StatusOK, serviceDesiredStateExport{
		Document: document,
		YAML:     string(rendered),
		Filename: buildDesiredStateFilename(shared.StringValue(service["name"])),
		Warnings: warnings,
	})
}

func buildServiceDesiredStateDocument(service bson.M, rules []bson.M, exportedAt string) (serviceDesiredStateDocument, []string) {
	ruleSpecs := make([]serviceDesiredStateRule, 0, len(rules))
	for _, rule := range rules {
		ruleSpecs = append(ruleSpecs, buildServiceDesiredStateRule(rule))
	}
	sort.Slice(ruleSpecs, func(i, j int) bool {
		if ruleSpecs[i].Environment != ruleSpecs[j].Environment {
			return ruleSpecs[i].Environment < ruleSpecs[j].Environment
		}
		if ruleSpecs[i].Name != ruleSpecs[j].Name {
			return ruleSpecs[i].Name < ruleSpecs[j].Name
		}
		return ruleSpecs[i].ID < ruleSpecs[j].ID
	})

	environmentKeys := buildDesiredStateEnvironmentKeys(service["environment"])
	warnings := make([]string, 0, 1)
	if len(environmentKeys) > 0 {
		warnings = append(warnings, "Environment variable values are not exported. The desired state document only includes environment keys.")
	}

	return serviceDesiredStateDocument{
		Kind:       "releasea.service.desired-state",
		APIVersion: "v1",
		Version:    1,
		ExportedAt: exportedAt,
		Service: serviceDesiredStateService{
			ID:        shared.StringValue(service["id"]),
			Name:      shared.StringValue(service["name"]),
			ProjectID: shared.StringValue(service["projectId"]),
			Type:      shared.StringValue(service["type"]),
			Spec:      buildServiceDesiredStateSpec(service, environmentKeys),
		},
		Rules: ruleSpecs,
	}, warnings
}

func buildServiceDesiredStateSpec(service bson.M, environmentKeys []string) serviceDesiredStateServiceSpec {
	workerTags := shared.NormalizeWorkerTags(shared.ToStringSlice(service["workerTags"]))
	sort.Strings(workerTags)

	sourceType := normalizeServiceSourceType(shared.StringValue(service["sourceType"]))
	if sourceType == "" {
		if strings.TrimSpace(shared.StringValue(service["repoUrl"])) != "" {
			sourceType = "git"
		} else if strings.TrimSpace(shared.StringValue(service["dockerImage"])) != "" {
			sourceType = "registry"
		}
	}

	spec := serviceDesiredStateServiceSpec{
		ManagementMode: normalizeServiceManagementMode(shared.StringValue(service["managementMode"])),
		SourceType:     sourceType,
		DeployTemplate: shared.StringValue(service["deployTemplateId"]),
		Runtime: serviceDesiredStateRuntimeSpec{
			Port:               shared.IntValue(service["port"]),
			HealthCheckPath:    shared.StringValue(service["healthCheckPath"]),
			Replicas:           shared.IntValue(service["replicas"]),
			MinReplicas:        shared.IntValue(service["minReplicas"]),
			MaxReplicas:        shared.IntValue(service["maxReplicas"]),
			CPU:                shared.IntValue(service["cpu"]),
			Memory:             shared.IntValue(service["memory"]),
			ProfileID:          shared.StringValue(service["profileId"]),
			WorkerTags:         workerTags,
			DeploymentStrategy: normalizeDesiredStateMap(service["deploymentStrategy"]),
		},
		Credentials: serviceDesiredStateCredentialRefs{
			SCMCredentialID:      shared.StringValue(service["scmCredentialId"]),
			RegistryCredentialID: shared.StringValue(service["registryCredentialId"]),
			SecretProviderID:     shared.StringValue(service["secretProviderId"]),
		},
		Environment: serviceDesiredStateEnvironmentSpec{
			Keys:           environmentKeys,
			ValuesExcluded: true,
		},
		Features: serviceDesiredStateFeatureSpec{
			AutoDeploy:              !serviceHasFalseValue(service["autoDeploy"]),
			IsActive:                !serviceHasFalseValue(service["isActive"]),
			PauseOnIdle:             shared.BoolValue(service["pauseOnIdle"]),
			PauseIdleTimeoutSeconds: shared.IntValue(service["pauseIdleTimeoutSeconds"]),
			RepoManaged:             shared.BoolValue(service["repoManaged"]),
		},
	}

	if repo := buildServiceDesiredStateRepoSpec(service); repo != nil {
		spec.Repo = repo
	}
	if image := buildServiceDesiredStateImageSpec(service); image != nil {
		spec.Image = image
	}
	if staticSite := buildServiceDesiredStateStaticSiteSpec(service); staticSite != nil {
		spec.StaticSite = staticSite
	}
	if schedule := buildServiceDesiredStateScheduleSpec(service); schedule != nil {
		spec.Schedule = schedule
	}

	return spec
}

func buildServiceDesiredStateRepoSpec(service bson.M) *serviceDesiredStateRepoSpec {
	spec := &serviceDesiredStateRepoSpec{
		URL:     shared.StringValue(service["repoUrl"]),
		Branch:  shared.StringValue(service["branch"]),
		RootDir: shared.StringValue(service["rootDir"]),
	}
	if spec.URL == "" && spec.Branch == "" && spec.RootDir == "" {
		return nil
	}
	return spec
}

func buildServiceDesiredStateImageSpec(service bson.M) *serviceDesiredStateImageSpec {
	spec := &serviceDesiredStateImageSpec{
		DockerImage:      shared.StringValue(service["dockerImage"]),
		DockerContext:    shared.StringValue(service["dockerContext"]),
		DockerfilePath:   shared.StringValue(service["dockerfilePath"]),
		DockerCommand:    shared.StringValue(service["dockerCommand"]),
		PreDeployCommand: shared.StringValue(service["preDeployCommand"]),
	}
	if spec.DockerImage == "" && spec.DockerContext == "" && spec.DockerfilePath == "" && spec.DockerCommand == "" && spec.PreDeployCommand == "" {
		return nil
	}
	return spec
}

func buildServiceDesiredStateStaticSiteSpec(service bson.M) *serviceDesiredStateStaticSiteSpec {
	spec := &serviceDesiredStateStaticSiteSpec{
		Framework:      shared.StringValue(service["framework"]),
		InstallCommand: shared.StringValue(service["installCommand"]),
		BuildCommand:   shared.StringValue(service["buildCommand"]),
		OutputDir:      shared.StringValue(service["outputDir"]),
		CacheTTL:       service["cacheTtl"],
	}
	if spec.Framework == "" && spec.InstallCommand == "" && spec.BuildCommand == "" && spec.OutputDir == "" && spec.CacheTTL == nil {
		return nil
	}
	return spec
}

func buildServiceDesiredStateScheduleSpec(service bson.M) *serviceDesiredStateScheduleSpec {
	spec := &serviceDesiredStateScheduleSpec{
		Cron:     shared.StringValue(service["scheduleCron"]),
		Timezone: shared.StringValue(service["scheduleTimezone"]),
		Command:  shared.StringValue(service["scheduleCommand"]),
		Retries:  shared.StringValue(service["scheduleRetries"]),
		Timeout:  shared.StringValue(service["scheduleTimeout"]),
	}
	if spec.Cron == "" && spec.Timezone == "" && spec.Command == "" && spec.Retries == "" && spec.Timeout == "" {
		return nil
	}
	return spec
}

func buildDesiredStateEnvironmentKeys(raw interface{}) []string {
	env := shared.MapPayload(raw)
	if len(env) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		keys = append(keys, trimmed)
	}
	sort.Strings(keys)
	return keys
}

func buildServiceDesiredStateRule(rule bson.M) serviceDesiredStateRule {
	gateways := shared.UniqueStrings(shared.ToStringSlice(rule["gateways"]))
	hosts := shared.UniqueStrings(shared.ToStringSlice(rule["hosts"]))
	paths := shared.UniqueStrings(shared.ToStringSlice(rule["paths"]))
	methods := shared.UniqueStrings(shared.ToStringSlice(rule["methods"]))
	sort.Strings(gateways)
	sort.Strings(hosts)
	sort.Strings(paths)
	sort.Strings(methods)

	return serviceDesiredStateRule{
		ID:          shared.StringValue(rule["id"]),
		Name:        shared.StringValue(rule["name"]),
		Environment: shared.StringValue(rule["environment"]),
		Protocol:    shared.StringValue(rule["protocol"]),
		Port:        shared.IntValue(rule["port"]),
		Hosts:       hosts,
		Paths:       paths,
		Methods:     methods,
		Gateways:    gateways,
		Publication: serviceDesiredStatePublicationSpec{
			Internal: desiredStateHasInternalGateway(gateways),
			External: desiredStateHasExternalGateway(gateways),
		},
		Status:          shared.StringValue(rule["status"]),
		Policy:          normalizeDesiredStateMap(rule["policy"]),
		LastPublishedAt: shared.StringValue(rule["lastPublishedAt"]),
	}
}

func normalizeDesiredStateMap(raw interface{}) map[string]interface{} {
	normalized := shared.MapPayload(raw)
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func desiredStateHasInternalGateway(gateways []string) bool {
	for _, gateway := range gateways {
		value := strings.ToLower(strings.TrimSpace(gateway))
		if value == "" {
			continue
		}
		if value == "mesh" || value == "internal" || strings.Contains(value, "releasea-internal-gateway") {
			return true
		}
	}
	return false
}

func desiredStateHasExternalGateway(gateways []string) bool {
	for _, gateway := range gateways {
		value := strings.ToLower(strings.TrimSpace(gateway))
		if value == "" || value == "mesh" {
			continue
		}
		if value == "external" {
			return true
		}
		if strings.Contains(value, "releasea-internal-gateway") {
			continue
		}
		return true
	}
	return false
}

func buildDesiredStateFilename(serviceName string) string {
	slug := make([]rune, 0, len(serviceName))
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(serviceName)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			slug = append(slug, char)
			lastDash = false
		case !lastDash:
			slug = append(slug, '-')
			lastDash = true
		}
	}
	filename := strings.Trim(string(slug), "-")
	if filename == "" {
		filename = "service"
	}
	return "releasea-service-" + filename + "-desired-state.yaml"
}

func serviceHasFalseValue(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return !typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "false")
	default:
		return false
	}
}
