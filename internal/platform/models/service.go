package models

import (
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type Service struct {
	ID                   string
	Name                 string
	Type                 string
	SourceType           string
	RepoURL              string
	Branch               string
	RootDir              string
	DockerImage          string
	DockerContext        string
	DockerfilePath       string
	DockerCommand        string
	PreDeployCommand     string
	Framework            string
	InstallCommand       string
	BuildCommand         string
	OutputDir            string
	CacheTTL             string
	ScheduleCron         string
	ScheduleTimezone     string
	ScheduleCommand      string
	ScheduleRetries      string
	ScheduleTimeout      string
	HealthCheckPath      string
	DeployTemplateID     string
	SecretProviderID     string
	ProjectID            string
	SCMCredentialID      string
	RegistryCredentialID string
	Port                 int
	Replicas             int
	MinReplicas          int
	MaxReplicas          int
	CPU                  int
	Memory               int
	RepoManaged          bool
	DeploymentStrategy   interface{}
	Environment          interface{}
}

func ServiceFromBSON(doc bson.M) Service {
	return Service{
		ID:                   shared.StringValue(doc["id"]),
		Name:                 shared.StringValue(doc["name"]),
		Type:                 shared.StringValue(doc["type"]),
		SourceType:           shared.StringValue(doc["sourceType"]),
		RepoURL:              shared.StringValue(doc["repoUrl"]),
		Branch:               shared.StringValue(doc["branch"]),
		RootDir:              shared.StringValue(doc["rootDir"]),
		DockerImage:          shared.StringValue(doc["dockerImage"]),
		DockerContext:        shared.StringValue(doc["dockerContext"]),
		DockerfilePath:       shared.StringValue(doc["dockerfilePath"]),
		DockerCommand:        shared.StringValue(doc["dockerCommand"]),
		PreDeployCommand:     shared.StringValue(doc["preDeployCommand"]),
		Framework:            shared.StringValue(doc["framework"]),
		InstallCommand:       shared.StringValue(doc["installCommand"]),
		BuildCommand:         shared.StringValue(doc["buildCommand"]),
		OutputDir:            shared.StringValue(doc["outputDir"]),
		CacheTTL:             shared.StringValue(doc["cacheTtl"]),
		ScheduleCron:         shared.StringValue(doc["scheduleCron"]),
		ScheduleTimezone:     shared.StringValue(doc["scheduleTimezone"]),
		ScheduleCommand:      shared.StringValue(doc["scheduleCommand"]),
		ScheduleRetries:      shared.StringValue(doc["scheduleRetries"]),
		ScheduleTimeout:      shared.StringValue(doc["scheduleTimeout"]),
		HealthCheckPath:      shared.StringValue(doc["healthCheckPath"]),
		DeployTemplateID:     shared.StringValue(doc["deployTemplateId"]),
		SecretProviderID:     shared.StringValue(doc["secretProviderId"]),
		ProjectID:            shared.StringValue(doc["projectId"]),
		SCMCredentialID:      shared.StringValue(doc["scmCredentialId"]),
		RegistryCredentialID: shared.StringValue(doc["registryCredentialId"]),
		Port:                 shared.IntValue(doc["port"]),
		Replicas:             shared.IntValue(doc["replicas"]),
		MinReplicas:          shared.IntValue(doc["minReplicas"]),
		MaxReplicas:          shared.IntValue(doc["maxReplicas"]),
		CPU:                  shared.IntValue(doc["cpu"]),
		Memory:               shared.IntValue(doc["memory"]),
		RepoManaged:          shared.BoolValue(doc["repoManaged"]),
		DeploymentStrategy:   doc["deploymentStrategy"],
		Environment:          doc["environment"],
	}
}

func (s Service) ToWorkerPayload() map[string]interface{} {
	return map[string]interface{}{
		"id":                 s.ID,
		"name":               s.Name,
		"type":               s.Type,
		"sourceType":         s.SourceType,
		"repoUrl":            s.RepoURL,
		"branch":             s.Branch,
		"rootDir":            s.RootDir,
		"dockerImage":        s.DockerImage,
		"dockerContext":      s.DockerContext,
		"dockerfilePath":     s.DockerfilePath,
		"dockerCommand":      s.DockerCommand,
		"preDeployCommand":   s.PreDeployCommand,
		"framework":          s.Framework,
		"installCommand":     s.InstallCommand,
		"buildCommand":       s.BuildCommand,
		"outputDir":          s.OutputDir,
		"cacheTtl":           s.CacheTTL,
		"scheduleCron":       s.ScheduleCron,
		"scheduleTimezone":   s.ScheduleTimezone,
		"scheduleCommand":    s.ScheduleCommand,
		"scheduleRetries":    s.ScheduleRetries,
		"scheduleTimeout":    s.ScheduleTimeout,
		"healthCheckPath":    s.HealthCheckPath,
		"port":               s.Port,
		"replicas":           s.Replicas,
		"minReplicas":        s.MinReplicas,
		"maxReplicas":        s.MaxReplicas,
		"cpu":                s.CPU,
		"memory":             s.Memory,
		"deploymentStrategy": s.DeploymentStrategy,
		"environment":        s.Environment,
		"deployTemplateId":   s.DeployTemplateID,
		"secretProviderId":   s.SecretProviderID,
		"repoManaged":        s.RepoManaged,
	}
}
