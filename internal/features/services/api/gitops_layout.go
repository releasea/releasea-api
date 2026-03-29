package services

import (
	"fmt"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type serviceGitOpsLayoutPreset struct {
	ID                  string   `json:"id"`
	Label               string   `json:"label"`
	Kind                string   `json:"kind"`
	Description         string   `json:"description"`
	PrimaryFilePath     string   `json:"primaryFilePath"`
	SupportingFilePaths []string `json:"supportingFilePaths,omitempty"`
	Available           bool     `json:"available"`
	AvailabilityReason  string   `json:"availabilityReason,omitempty"`
}

func buildServiceGitOpsLayoutPresets(service bson.M) []serviceGitOpsLayoutPreset {
	serviceName := shared.StringValue(service["name"])
	isManaged := !isObservedService(service)
	hasRepo := strings.TrimSpace(shared.StringValue(service["repoUrl"])) != ""

	available := isManaged && hasRepo
	reason := ""
	switch {
	case !isManaged:
		reason = "Managed mode is required before Releasea can operate on repository layouts."
	case !hasRepo:
		reason = "Configure a repository URL before Releasea can work with GitOps layouts."
	}

	return []serviceGitOpsLayoutPreset{
		{
			ID:                 "legacy",
			Label:              "Direct desired-state export",
			Kind:               "direct",
			Description:        "Writes the desired-state file directly under the legacy Releasea GitOps path.",
			PrimaryFilePath:    defaultGitOpsLegacyFilePath(serviceName),
			Available:          available,
			AvailabilityReason: reason,
		},
		{
			ID:              "argocd",
			Label:           "Argo CD starter",
			Kind:            "starter",
			Description:     "Packages the desired-state export with Kustomize and an Argo CD Application starter manifest.",
			PrimaryFilePath: defaultGitOpsArgoCDDesiredStateFilePath(serviceName),
			SupportingFilePaths: []string{
				defaultGitOpsArgoCDKustomizationFilePath(serviceName),
				defaultGitOpsArgoCDApplicationFilePath(serviceName),
			},
			Available:          available,
			AvailabilityReason: reason,
		},
		{
			ID:              "flux",
			Label:           "Flux starter",
			Kind:            "starter",
			Description:     "Packages the desired-state export with Kustomize plus Flux GitRepository and Kustomization starters.",
			PrimaryFilePath: defaultGitOpsArgoCDDesiredStateFilePath(serviceName),
			SupportingFilePaths: []string{
				defaultGitOpsArgoCDKustomizationFilePath(serviceName),
				defaultGitOpsFluxGitRepositoryFilePath(serviceName),
				defaultGitOpsFluxKustomizationFilePath(serviceName),
			},
			Available:          available,
			AvailabilityReason: reason,
		},
	}
}

func findServiceGitOpsLayoutPreset(service bson.M, presetID string) (serviceGitOpsLayoutPreset, bool) {
	for _, preset := range buildServiceGitOpsLayoutPresets(service) {
		if preset.ID == strings.TrimSpace(strings.ToLower(presetID)) {
			return preset, true
		}
	}
	return serviceGitOpsLayoutPreset{}, false
}

func resolveServiceGitOpsDriftPaths(service bson.M, explicitFilePath string) []string {
	if trimmed := strings.Trim(strings.TrimSpace(explicitFilePath), "/"); trimmed != "" {
		return []string{trimmed}
	}

	paths := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, preset := range buildServiceGitOpsLayoutPresets(service) {
		if preset.PrimaryFilePath == "" {
			continue
		}
		if _, ok := seen[preset.PrimaryFilePath]; ok {
			continue
		}
		seen[preset.PrimaryFilePath] = struct{}{}
		paths = append(paths, preset.PrimaryFilePath)
	}
	return paths
}

func defaultGitOpsLegacyFilePath(serviceName string) string {
	return ".releasea/gitops/" + sanitizeGitOpsPathSegment(serviceName) + ".desired-state.yaml"
}

func defaultGitOpsArgoCDDesiredStateFilePath(serviceName string) string {
	return ".releasea/gitops/" + sanitizeGitOpsPathSegment(serviceName) + "/desired-state.yaml"
}

func defaultGitOpsArgoCDKustomizationFilePath(serviceName string) string {
	return ".releasea/gitops/" + sanitizeGitOpsPathSegment(serviceName) + "/kustomization.yaml"
}

func defaultGitOpsArgoCDApplicationFilePath(serviceName string) string {
	return ".releasea/gitops/argocd/" + sanitizeGitOpsPathSegment(serviceName) + "-application.yaml"
}

func defaultGitOpsFluxGitRepositoryFilePath(serviceName string) string {
	return ".releasea/gitops/flux/" + sanitizeGitOpsPathSegment(serviceName) + "-gitrepository.yaml"
}

func defaultGitOpsFluxKustomizationFilePath(serviceName string) string {
	return ".releasea/gitops/flux/" + sanitizeGitOpsPathSegment(serviceName) + "-kustomization.yaml"
}

func defaultGitOpsArgoCDCommitMessage(serviceName, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("chore(gitops): add Argo CD starter for %s", strings.TrimSpace(serviceName))
}

func defaultGitOpsArgoCDPRTitle(serviceName, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("chore(gitops): add Argo CD starter for %s", strings.TrimSpace(serviceName))
}

func defaultGitOpsArgoCDPRBody(service bson.M, warnings []string, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	lines := []string{
		"## Releasea Argo CD starter",
		"",
		"This pull request was created by Releasea to bootstrap an Argo CD reconciliation path for the managed service desired-state export.",
		"",
		"- Service: `" + shared.StringValue(service["name"]) + "`",
		"- Project ID: `" + shared.StringValue(service["projectId"]) + "`",
		"- Repository: `" + shared.StringValue(service["repoUrl"]) + "`",
		"",
		"Starter contents:",
		"- desired-state YAML under `.releasea/gitops/<service>/desired-state.yaml`",
		"- Kustomize config that packages the desired state as a cluster ConfigMap",
		"- Argo CD Application manifest pointing at that folder",
		"",
		"This starter gives Argo CD a real repository reconciliation target for the exported desired-state artifact. It does not replace Releasea runtime deployments or render workload manifests directly.",
	}
	if len(warnings) > 0 {
		lines = append(lines, "", "### Notes")
		for _, warning := range warnings {
			lines = append(lines, "- "+warning)
		}
	}
	return strings.Join(lines, "\n")
}

func defaultGitOpsFluxCommitMessage(serviceName, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("chore(gitops): add Flux starter for %s", strings.TrimSpace(serviceName))
}

func defaultGitOpsFluxPRTitle(serviceName, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("chore(gitops): add Flux starter for %s", strings.TrimSpace(serviceName))
}

func defaultGitOpsFluxPRBody(service bson.M, warnings []string, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	lines := []string{
		"## Releasea Flux starter",
		"",
		"This pull request was created by Releasea to bootstrap a Flux reconciliation path for the managed service desired-state export.",
		"",
		"- Service: `" + shared.StringValue(service["name"]) + "`",
		"- Project ID: `" + shared.StringValue(service["projectId"]) + "`",
		"- Repository: `" + shared.StringValue(service["repoUrl"]) + "`",
		"",
		"Starter contents:",
		"- desired-state YAML under `.releasea/gitops/<service>/desired-state.yaml`",
		"- Kustomize config that packages the desired state as a cluster ConfigMap",
		"- Flux `GitRepository` manifest pointing at the service repository",
		"- Flux `Kustomization` manifest targeting the desired-state folder",
		"",
		"This starter gives Flux a real repository reconciliation target for the exported desired-state artifact. It does not replace Releasea runtime deployments or render workload manifests directly.",
	}
	if len(warnings) > 0 {
		lines = append(lines, "", "### Notes")
		for _, warning := range warnings {
			lines = append(lines, "- "+warning)
		}
	}
	return strings.Join(lines, "\n")
}
