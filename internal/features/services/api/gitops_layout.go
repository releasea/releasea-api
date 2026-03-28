package services

import (
	"fmt"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

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
