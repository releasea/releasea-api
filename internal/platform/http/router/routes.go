package router

import (
	"net/url"
	"os"
	"strings"
	"time"

	ai "releaseaapi/internal/features/ai/api"
	audit "releaseaapi/internal/features/audit/api"
	auth "releaseaapi/internal/features/auth/api"
	credentials "releaseaapi/internal/features/credentials/api"
	deploys "releaseaapi/internal/features/deploys/api"
	environments "releaseaapi/internal/features/environments/api"
	externalendpoints "releaseaapi/internal/features/externalendpoints/api"
	governance "releaseaapi/internal/features/governance/api"
	identity "releaseaapi/internal/features/identity/api"
	observability "releaseaapi/internal/features/observability/api"
	operations "releaseaapi/internal/features/operations/api"
	profile "releaseaapi/internal/features/profile/api"
	projects "releaseaapi/internal/features/projects/api"
	ruledeploys "releaseaapi/internal/features/ruledeploys/api"
	rules "releaseaapi/internal/features/rules/api"
	scm "releaseaapi/internal/features/scm/api"
	services "releaseaapi/internal/features/services/api"
	settings "releaseaapi/internal/features/settings/api"
	teams "releaseaapi/internal/features/teams/api"
	templates "releaseaapi/internal/features/templates/api"
	workers "releaseaapi/internal/features/workers/api"
	platformauth "releaseaapi/internal/platform/auth"
	platformsecurity "releaseaapi/internal/platform/http/security"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {
	corsOrigins := []string{
		"http://localhost:3000",
		"http://localhost:5173",
	}
	envOrigins := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	allowAllOrigins := false
	if envOrigins != "" {
		if envOrigins == "*" {
			allowAllOrigins = true
		} else {
			corsOrigins = splitAndTrim(envOrigins)
		}
	}

	corsConfig := cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "X-Request-Id", "X-Request-ID", "X-Correlation-ID", "X-CSRF-Token", "Idempotency-Key", "Last-Event-ID"},
		ExposeHeaders:    []string{"Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
		AllowAllOrigins:  allowAllOrigins,
	}

	corsConfig.AllowOriginFunc = func(origin string) bool {
		if origin == "" || allowAllOrigins {
			return true
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := parsed.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}

	r.Use(cors.New(corsConfig))

	v1 := r.Group("/api/v1")
	v1.Use(platformsecurity.CorrelationContextMiddleware())
	v1.Use(platformsecurity.RequiredBrowserHeadersMiddleware())
	registerPublicRoutes(v1)

	protected := v1.Group("/")
	protected.Use(platformauth.AuthMiddleware())
	protected.Use(platformsecurity.CSRFMiddlewareForUserMutations())
	registerProtectedRoutes(protected)
}

func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func registerPublicRoutes(rg *gin.RouterGroup) {
	rg.GET("/health", shared.Readiness)
	rg.GET("/health/live", shared.Health)
	rg.GET("/health/ready", shared.Readiness)
	authGroup := rg.Group("/auth")
	authGroup.Use(platformauth.AuthRateLimitMiddleware())
	authGroup.GET("/csrf", auth.CSRFToken)
	authGroup.POST("/login", auth.Login)
	authGroup.POST("/signup", auth.Signup)
	authGroup.POST("/logout", platformsecurity.CSRFMiddleware(), auth.Logout)
	authGroup.POST("/refresh", platformsecurity.CSRFMiddleware(), auth.Refresh)
	authGroup.POST("/password/reset", auth.RequestPasswordReset)
	authGroup.GET("/password/reset/validate", auth.ValidatePasswordReset)
	authGroup.POST("/password/reset/confirm", auth.ConfirmPasswordReset)
	authGroup.GET("/sso/config", identity.GetSSOConfig)
	authGroup.GET("/sso/start", identity.StartSSO)
	authGroup.GET("/sso/callback", identity.CompleteSSO)
	authGroup.POST("/sso/exchange", identity.ExchangeSSOTicket)
}

func registerProtectedRoutes(rg *gin.RouterGroup) {
	registerWorkerRoutes(rg)
	registerTeamRoutes(rg)
	registerProjectRoutes(rg)
	registerServiceRoutes(rg)
	registerDeployRoutes(rg)
	registerRuleRoutes(rg)
	registerRuleDeployRoutes(rg)
	registerObservabilityRoutes(rg)
	registerCredentialsRoutes(rg)
	registerScmRoutes(rg)
	registerEnvironmentRoutes(rg)
	registerExternalEndpointRoutes(rg)
	registerTemplateRoutes(rg)
	registerProfileRoutes(rg)
	registerSettingsRoutes(rg)
	registerGovernanceRoutes(rg)
	registerIdentityRoutes(rg)
	registerAuditRoutes(rg)
	registerOperationsRoutes(rg)
	registerAIRoutes(rg)
}

func registerAIRoutes(rg *gin.RouterGroup) {
	admin := platformauth.RequireRoles("admin")
	rg.GET("/ai/providers", admin, ai.ListProviders)
	rg.GET("/ai/providers/available", platformauth.RequireRoles("admin", "developer"), ai.ListAvailableProviders)
	rg.POST("/ai/providers", admin, ai.CreateProvider)
	rg.PUT("/ai/providers/:id", admin, ai.UpdateProvider)
	rg.DELETE("/ai/providers/:id", admin, ai.DeleteProvider)
	rg.POST("/ai/providers/:id/test", admin, ai.TestProvider)
	rg.GET("/ai/usage", admin, ai.GetUsage)
	rg.GET("/services/:id/ai/analyses", ai.ListServiceAnalyses)
	rg.POST("/services/:id/ai/analyses", platformauth.RequireRoles("admin", "developer"), ai.CreateServiceAnalysis)
}

func registerWorkerRoutes(rg *gin.RouterGroup) {
	rg.POST("/workers/heartbeat", workers.Heartbeat)
	rg.POST("/workers/auth", workers.WorkerAuth)
	rg.POST("/workers/autodeploy/lease", workers.AcquireAutoDeployLease)
	rg.POST("/workers/credentials", credentials.WorkerCredentials)
	rg.POST("/workers/services/:id/runtime", services.UpdateServiceRuntime)
	rg.POST("/workers/services/:id/blue-green/primary", services.UpdateBlueGreenPrimary)
	rg.GET("/workers/bootstrap-profile", workers.GetWorkerBootstrapProfile)
	rg.GET("/workers", workers.GetWorkers)
	rg.GET("/workers/pools", workers.GetWorkerPools)
	rg.GET("/workers/pool-control", workers.GetCurrentWorkerPoolControl)
	rg.POST("/workers/pools/:id/maintenance", platformauth.RequireRoles("admin"), workers.SetWorkerPoolMaintenance)
	rg.POST("/workers/pools/:id/drain", platformauth.RequireRoles("admin"), workers.SetWorkerPoolDrain)
	rg.GET("/workers/discovered-workloads", workers.GetDiscoveredWorkloads)
	rg.PUT("/workers/:id", platformauth.RequireRoles("admin"), workers.UpdateWorker)
	rg.DELETE("/workers/:id", platformauth.RequireRoles("admin"), workers.DeleteWorker)
	rg.POST("/workers/:id/restart", platformauth.RequireRoles("admin"), workers.RestartWorker)
	rg.GET("/workers/registrations", workers.GetWorkerRegistrations)
	rg.POST("/workers/registrations", platformauth.RequireRoles("admin"), workers.CreateWorkerRegistration)
	rg.DELETE("/workers/registrations/:id", platformauth.RequireRoles("admin"), workers.DeleteWorkerRegistration)
	rg.POST("/workers/builds", workers.RegisterBuild)
}

func registerTeamRoutes(rg *gin.RouterGroup) {
	rg.GET("/teams", teams.GetTeams)
	rg.POST("/teams", platformauth.RequireRoles("admin"), teams.CreateTeam)
	rg.PUT("/teams/:id", platformauth.RequireRoles("admin"), teams.UpdateTeam)
	rg.DELETE("/teams/:id", platformauth.RequireRoles("admin"), teams.DeleteTeam)
}

func registerProjectRoutes(rg *gin.RouterGroup) {
	rg.GET("/projects", projects.GetProjects)
	rg.POST("/projects", platformauth.RequireRoles("admin", "developer"), projects.CreateProject)
	rg.DELETE("/projects/:id", platformauth.RequireRoles("admin", "developer"), projects.DeleteProject)
	rg.PUT("/projects/:id", platformauth.RequireRoles("admin", "developer"), projects.UpdateProject)
}

func registerServiceRoutes(rg *gin.RouterGroup) {
	rg.GET("/services", services.GetServices)
	rg.GET("/services/status", services.GetServicesStatusSnapshot)
	rg.GET("/services/status/stream", services.StreamServicesStatus)
	rg.POST("/services", platformauth.RequireRoles("admin", "developer"), services.CreateService)
	rg.GET("/services/:id", services.GetService)
	rg.GET("/services/:id/status", services.GetServiceStatusSnapshot)
	rg.GET("/services/:id/status/stream", services.StreamServiceStatus)
	rg.PUT("/services/:id", platformauth.RequireRoles("admin", "developer"), services.UpdateService)
	rg.DELETE("/services/:id", platformauth.RequireRoles("admin", "developer"), services.DeleteService)
	rg.POST("/services/:id/rules", platformauth.RequireRoles("admin", "developer"), rules.CreateServiceRule)
	rg.GET("/services/:id/metrics", services.GetServiceMetrics)
	rg.GET("/services/:id/logs", services.GetServiceLogs)
	rg.GET("/services/:id/pods", services.GetServicePods)
	rg.POST("/services/:id/deploys", platformauth.RequireRoles("admin", "developer"), platformsecurity.RequireIdempotencyKey(), services.CreateDeploy)
	rg.POST("/services/:id/promote-canary", platformauth.RequireRoles("admin", "developer"), platformsecurity.RequireIdempotencyKey(), services.PromoteCanary)
	rg.GET("/services/:id/builds", services.GetServiceBuilds)
	rg.GET("/services/:id/governance-events", services.GetServiceGovernanceEvents)
	rg.GET("/services/:id/deploy-policy-check", services.GetServiceDeployPolicyCheck)
	rg.GET("/services/:id/desired-state", services.GetServiceDesiredState)
	rg.GET("/services/:id/desired-state/validation", services.GetServiceDesiredStateValidation)
	rg.GET("/services/:id/gitops/layout-presets", services.GetServiceGitOpsLayoutPresets)
	rg.GET("/services/:id/gitops/repository-policy-check", services.GetServiceGitOpsRepositoryPolicyCheck)
	rg.GET("/services/:id/gitops/drift", services.GetServiceGitOpsDrift)
	rg.GET("/services/:id/gitops/timeline", services.GetServiceGitOpsTimeline)
	rg.POST("/services/:id/gitops/pull-requests", platformauth.RequireRoles("admin", "developer"), services.CreateServiceGitOpsPullRequest)
	rg.POST("/services/:id/gitops/argocd/pull-requests", platformauth.RequireRoles("admin", "developer"), services.CreateServiceArgoCDGitOpsPullRequest)
	rg.POST("/services/:id/gitops/flux/pull-requests", platformauth.RequireRoles("admin", "developer"), services.CreateServiceFluxGitOpsPullRequest)
}

func registerDeployRoutes(rg *gin.RouterGroup) {
	rg.GET("/deploys", deploys.GetDeploys)
	rg.POST("/deploys/:id/logs", deploys.AppendDeployLogs)
}

func registerRuleRoutes(rg *gin.RouterGroup) {
	rg.GET("/rules", rules.GetRules)
	rg.POST("/rules", platformauth.RequireRoles("admin", "developer"), rules.CreateRule)
	rg.GET("/rules/:id", rules.GetRule)
	rg.GET("/rules/:id/publish-policy-check", rules.GetRulePublishPolicyCheck)
	rg.PUT("/rules/:id", platformauth.RequireRoles("admin", "developer"), rules.UpdateRule)
	rg.DELETE("/rules/:id", platformauth.RequireRoles("admin", "developer"), rules.DeleteRule)
	rg.POST("/rules/:id/publish", platformauth.RequireRoles("admin", "developer"), rules.PublishRule)
	rg.POST("/rules/:id/logs", rules.AppendRuleLogs)
}

func registerRuleDeployRoutes(rg *gin.RouterGroup) {
	rg.GET("/rule-deploys", ruledeploys.GetRuleDeploys)
	rg.POST("/rule-deploys/:id/logs", ruledeploys.AppendRuleDeployLogs)
}

func registerObservabilityRoutes(rg *gin.RouterGroup) {
	rg.GET("/logs", observability.GetLogs)
	rg.GET("/observability/health", observability.ObservabilityHealth)
	rg.GET("/observability/control-plane", observability.GetControlPlaneMetrics)
}

func registerCredentialsRoutes(rg *gin.RouterGroup) {
	rg.GET("/credentials/scm", platformauth.RequireRoles("admin"), credentials.GetScmCredentials)
	rg.POST("/credentials/scm", platformauth.RequireRoles("admin"), credentials.CreateScmCredential)
	rg.PUT("/credentials/scm/:id", platformauth.RequireRoles("admin"), credentials.UpdateScmCredential)
	rg.DELETE("/credentials/scm/:id", platformauth.RequireRoles("admin"), credentials.DeleteScmCredential)
	rg.GET("/credentials/registry", platformauth.RequireRoles("admin"), credentials.GetRegistryCredentials)
	rg.POST("/credentials/registry", platformauth.RequireRoles("admin"), credentials.CreateRegistryCredential)
	rg.PUT("/credentials/registry/:id", platformauth.RequireRoles("admin"), credentials.UpdateRegistryCredential)
	rg.DELETE("/credentials/registry/:id", platformauth.RequireRoles("admin"), credentials.DeleteRegistryCredential)
}

func registerScmRoutes(rg *gin.RouterGroup) {
	rg.POST("/scm/github/template-repos", platformauth.RequireRoles("admin", "developer"), scm.CreateTemplateRepo)
	rg.GET("/scm/github/template-repos/availability", scm.CheckTemplateRepoAvailability)
	rg.GET("/scm/commits", scm.ListCommits)
}

func registerEnvironmentRoutes(rg *gin.RouterGroup) {
	rg.GET("/regions", environments.GetRegions)
	rg.GET("/environments", environments.GetEnvironments)
	rg.POST("/environments", platformauth.RequireRoles("admin"), environments.CreateEnvironment)
	rg.PUT("/environments/:id", platformauth.RequireRoles("admin"), environments.UpdateEnvironment)
	rg.DELETE("/environments/:id", platformauth.RequireRoles("admin"), environments.DeleteEnvironment)
	rg.GET("/environments/:id/lock", environments.CheckEnvironmentLock)
	rg.GET("/deploy-templates", environments.GetDeployTemplates)
	rg.GET("/deploy-templates/:id", environments.GetDeployTemplate)
	rg.POST("/deploy-templates", platformauth.RequireRoles("admin"), environments.CreateDeployTemplate)
	rg.PUT("/deploy-templates/:id", platformauth.RequireRoles("admin"), environments.UpdateDeployTemplate)
	rg.DELETE("/deploy-templates/:id", platformauth.RequireRoles("admin"), environments.DeleteDeployTemplate)
}

func registerExternalEndpointRoutes(rg *gin.RouterGroup) {
	rg.GET("/external-endpoints", externalendpoints.GetExternalEndpoints)
	rg.POST("/external-endpoints", platformauth.RequireRoles("admin"), externalendpoints.CreateExternalEndpoint)
	rg.PUT("/external-endpoints/:id", platformauth.RequireRoles("admin"), externalendpoints.UpdateExternalEndpoint)
	rg.DELETE("/external-endpoints/:id", platformauth.RequireRoles("admin"), externalendpoints.DeleteExternalEndpoint)
}

func registerTemplateRoutes(rg *gin.RouterGroup) {
	rg.GET("/templates", templates.ListTemplates)
	rg.GET("/templates/:id", templates.GetTemplate)
	rg.POST("/templates/verify", platformauth.RequireRoles("admin"), templates.VerifyTemplates)
	rg.POST("/templates", platformauth.RequireRoles("admin"), templates.CreateTemplate)
	rg.PUT("/templates/:id", platformauth.RequireRoles("admin"), templates.UpdateTemplate)
	rg.DELETE("/templates/:id", platformauth.RequireRoles("admin"), templates.DeleteTemplate)
}

func registerProfileRoutes(rg *gin.RouterGroup) {
	rg.GET("/profile", profile.GetProfile)
	rg.PUT("/profile", profile.UpdateProfile)
	rg.POST("/profile/password", profile.ChangePassword)
	rg.DELETE("/profile/sessions/:id", profile.RevokeSession)
	rg.POST("/profile/providers/:id", profile.ConnectProvider)
	rg.DELETE("/profile/providers/:id", profile.DisconnectProvider)
	rg.DELETE("/profile", profile.DeleteProfile)
}

func registerSettingsRoutes(rg *gin.RouterGroup) {
	rg.GET("/settings/providers/catalog", settings.GetProviderCatalog)
	rg.GET("/settings/providers/status", settings.GetProviderStatus)
	rg.GET("/settings/providers/health", settings.GetProviderHealth)
	rg.GET("/settings/platform", platformauth.RequireRoles("admin"), settings.GetPlatformSettings)
	rg.PUT("/settings/platform", platformauth.RequireRoles("admin"), settings.UpdatePlatformSettings)
	rg.GET("/runtime-profiles", settings.GetRuntimeProfiles)
	rg.POST("/runtime-profiles", platformauth.RequireRoles("admin"), settings.CreateRuntimeProfile)
	rg.PUT("/runtime-profiles/:id", platformauth.RequireRoles("admin"), settings.UpdateRuntimeProfile)
	rg.DELETE("/runtime-profiles/:id", platformauth.RequireRoles("admin"), settings.DeleteRuntimeProfile)
}

func registerGovernanceRoutes(rg *gin.RouterGroup) {
	rg.GET("/governance/settings", governance.GetGovernanceSettings)
	rg.PUT("/governance/settings", platformauth.RequireRoles("admin"), governance.UpdateGovernanceSettings)
	rg.GET("/governance/approvals", governance.GetGovernanceApprovals)
	rg.POST("/governance/approvals", governance.CreateGovernanceApproval)
	rg.POST("/governance/approvals/:id/review", platformauth.RequireRoles("admin"), governance.ReviewGovernanceApproval)
	rg.DELETE("/governance/approvals/:id", platformauth.RequireRoles("admin"), governance.DeleteGovernanceApproval)
	rg.GET("/governance/exceptions", governance.GetGovernanceExceptions)
	rg.POST("/governance/exceptions", platformauth.RequireRoles("admin"), governance.CreateGovernanceException)
	rg.DELETE("/governance/exceptions/:id", platformauth.RequireRoles("admin"), governance.RevokeGovernanceException)
	rg.GET("/governance/audit", platformauth.RequireRoles("admin"), governance.GetGovernanceAudit)
}

func registerIdentityRoutes(rg *gin.RouterGroup) {
	admin := platformauth.RequireRoles("admin")
	rg.GET("/identity/config", admin, identity.GetIdpConfig)
	rg.PUT("/identity/config", admin, identity.UpdateIdpConfig)
	rg.GET("/identity/connections", admin, identity.GetIdpConnections)
	rg.POST("/identity/connections", admin, identity.CreateIdpConnection)
	rg.PUT("/identity/connections/:id", admin, identity.UpdateIdpConnection)
	rg.DELETE("/identity/connections/:id", admin, identity.DeleteIdpConnection)
	rg.GET("/identity/mappings", admin, identity.GetGroupMappings)
	rg.POST("/identity/mappings", admin, identity.CreateGroupMapping)
	rg.PUT("/identity/mappings/:id", admin, identity.UpdateGroupMapping)
	rg.DELETE("/identity/mappings/:id", admin, identity.DeleteGroupMapping)
	rg.POST("/identity/mappings/sync", admin, identity.SyncGroupMappings)
	rg.GET("/identity/sessions", admin, identity.GetIdpSessions)
	rg.DELETE("/identity/sessions/:id", admin, identity.RevokeIdpSession)
	rg.DELETE("/identity/sessions", admin, identity.RevokeAllIdpSessions)
	rg.GET("/identity/audit", admin, identity.GetIdpAudit)
	rg.POST("/identity/test/:protocol", admin, identity.TestIdpConnection)
}

func registerAuditRoutes(rg *gin.RouterGroup) {
	rg.GET("/audit", platformauth.RequireRoles("admin"), audit.GetAuditEvents)
}

func registerOperationsRoutes(rg *gin.RouterGroup) {
	rg.GET("/operations", operations.GetOperations)
	rg.GET("/operations/:id", operations.GetOperation)
	rg.POST("/operations/recover-stale-claims", operations.RecoverStaleOperationClaims)
	rg.POST("/operations/:id/status", operations.UpdateOperationStatus)
}
