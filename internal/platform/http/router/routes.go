package router

import (
	"net/url"
	"os"
	"strings"
	"time"

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
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "X-Request-Id", "X-Request-ID", "X-Correlation-ID", "X-CSRF-Token", "Idempotency-Key"},
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
	rg.GET("/health", shared.Health)
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
	rg.GET("/workers/discovered-workloads", workers.GetDiscoveredWorkloads)
	rg.PUT("/workers/:id", workers.UpdateWorker)
	rg.DELETE("/workers/:id", workers.DeleteWorker)
	rg.POST("/workers/:id/restart", workers.RestartWorker)
	rg.GET("/workers/registrations", workers.GetWorkerRegistrations)
	rg.POST("/workers/registrations", workers.CreateWorkerRegistration)
	rg.DELETE("/workers/registrations/:id", workers.DeleteWorkerRegistration)
	rg.POST("/workers/builds", workers.RegisterBuild)
}

func registerTeamRoutes(rg *gin.RouterGroup) {
	rg.GET("/teams", teams.GetTeams)
	rg.POST("/teams", teams.CreateTeam)
	rg.PUT("/teams/:id", teams.UpdateTeam)
	rg.DELETE("/teams/:id", teams.DeleteTeam)
}

func registerProjectRoutes(rg *gin.RouterGroup) {
	rg.GET("/projects", projects.GetProjects)
	rg.POST("/projects", projects.CreateProject)
	rg.DELETE("/projects/:id", projects.DeleteProject)
	rg.PUT("/projects/:id", projects.UpdateProject)
}

func registerServiceRoutes(rg *gin.RouterGroup) {
	rg.GET("/services", services.GetServices)
	rg.GET("/services/status", services.GetServicesStatusSnapshot)
	rg.GET("/services/status/stream", services.StreamServicesStatus)
	rg.POST("/services", services.CreateService)
	rg.GET("/services/:id", services.GetService)
	rg.GET("/services/:id/status", services.GetServiceStatusSnapshot)
	rg.GET("/services/:id/status/stream", services.StreamServiceStatus)
	rg.PUT("/services/:id", services.UpdateService)
	rg.DELETE("/services/:id", services.DeleteService)
	rg.POST("/services/:id/rules", rules.CreateServiceRule)
	rg.GET("/services/:id/metrics", services.GetServiceMetrics)
	rg.GET("/services/:id/logs", services.GetServiceLogs)
	rg.GET("/services/:id/pods", services.GetServicePods)
	rg.POST("/services/:id/deploys", platformsecurity.RequireIdempotencyKey(), services.CreateDeploy)
	rg.POST("/services/:id/promote-canary", platformsecurity.RequireIdempotencyKey(), services.PromoteCanary)
	rg.GET("/services/:id/builds", services.GetServiceBuilds)
	rg.GET("/services/:id/governance-events", services.GetServiceGovernanceEvents)
	rg.GET("/services/:id/deploy-policy-check", services.GetServiceDeployPolicyCheck)
	rg.GET("/services/:id/desired-state", services.GetServiceDesiredState)
	rg.GET("/services/:id/gitops/drift", services.GetServiceGitOpsDrift)
	rg.POST("/services/:id/gitops/pull-requests", services.CreateServiceGitOpsPullRequest)
	rg.POST("/services/:id/gitops/argocd/pull-requests", services.CreateServiceArgoCDGitOpsPullRequest)
}

func registerDeployRoutes(rg *gin.RouterGroup) {
	rg.GET("/deploys", deploys.GetDeploys)
	rg.POST("/deploys/:id/logs", deploys.AppendDeployLogs)
}

func registerRuleRoutes(rg *gin.RouterGroup) {
	rg.GET("/rules", rules.GetRules)
	rg.POST("/rules", rules.CreateRule)
	rg.GET("/rules/:id", rules.GetRule)
	rg.GET("/rules/:id/publish-policy-check", rules.GetRulePublishPolicyCheck)
	rg.PUT("/rules/:id", rules.UpdateRule)
	rg.DELETE("/rules/:id", rules.DeleteRule)
	rg.POST("/rules/:id/publish", rules.PublishRule)
	rg.POST("/rules/:id/logs", rules.AppendRuleLogs)
}

func registerRuleDeployRoutes(rg *gin.RouterGroup) {
	rg.GET("/rule-deploys", ruledeploys.GetRuleDeploys)
	rg.POST("/rule-deploys/:id/logs", ruledeploys.AppendRuleDeployLogs)
}

func registerObservabilityRoutes(rg *gin.RouterGroup) {
	rg.GET("/logs", observability.GetLogs)
	rg.GET("/observability/health", observability.ObservabilityHealth)
}

func registerCredentialsRoutes(rg *gin.RouterGroup) {
	rg.GET("/credentials/scm", credentials.GetScmCredentials)
	rg.POST("/credentials/scm", credentials.CreateScmCredential)
	rg.PUT("/credentials/scm/:id", credentials.UpdateScmCredential)
	rg.DELETE("/credentials/scm/:id", credentials.DeleteScmCredential)
	rg.GET("/credentials/registry", credentials.GetRegistryCredentials)
	rg.POST("/credentials/registry", credentials.CreateRegistryCredential)
	rg.PUT("/credentials/registry/:id", credentials.UpdateRegistryCredential)
	rg.DELETE("/credentials/registry/:id", credentials.DeleteRegistryCredential)
}

func registerScmRoutes(rg *gin.RouterGroup) {
	rg.POST("/scm/github/template-repos", scm.CreateTemplateRepo)
	rg.GET("/scm/github/template-repos/availability", scm.CheckTemplateRepoAvailability)
	rg.GET("/scm/commits", scm.ListCommits)
}

func registerEnvironmentRoutes(rg *gin.RouterGroup) {
	rg.GET("/regions", environments.GetRegions)
	rg.GET("/environments", environments.GetEnvironments)
	rg.POST("/environments", environments.CreateEnvironment)
	rg.PUT("/environments/:id", environments.UpdateEnvironment)
	rg.DELETE("/environments/:id", environments.DeleteEnvironment)
	rg.GET("/environments/:id/lock", environments.CheckEnvironmentLock)
	rg.GET("/deploy-templates", environments.GetDeployTemplates)
	rg.GET("/deploy-templates/:id", environments.GetDeployTemplate)
	rg.POST("/deploy-templates", environments.CreateDeployTemplate)
	rg.PUT("/deploy-templates/:id", environments.UpdateDeployTemplate)
	rg.DELETE("/deploy-templates/:id", environments.DeleteDeployTemplate)
}

func registerExternalEndpointRoutes(rg *gin.RouterGroup) {
	rg.GET("/external-endpoints", externalendpoints.GetExternalEndpoints)
	rg.POST("/external-endpoints", externalendpoints.CreateExternalEndpoint)
	rg.PUT("/external-endpoints/:id", externalendpoints.UpdateExternalEndpoint)
	rg.DELETE("/external-endpoints/:id", externalendpoints.DeleteExternalEndpoint)
}

func registerTemplateRoutes(rg *gin.RouterGroup) {
	rg.GET("/templates", templates.ListTemplates)
	rg.GET("/templates/:id", templates.GetTemplate)
	rg.POST("/templates", templates.CreateTemplate)
	rg.PUT("/templates/:id", templates.UpdateTemplate)
	rg.DELETE("/templates/:id", templates.DeleteTemplate)
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
	rg.GET("/settings/platform", settings.GetPlatformSettings)
	rg.PUT("/settings/platform", settings.UpdatePlatformSettings)
	rg.GET("/runtime-profiles", settings.GetRuntimeProfiles)
	rg.POST("/runtime-profiles", settings.CreateRuntimeProfile)
	rg.PUT("/runtime-profiles/:id", settings.UpdateRuntimeProfile)
	rg.DELETE("/runtime-profiles/:id", settings.DeleteRuntimeProfile)
}

func registerGovernanceRoutes(rg *gin.RouterGroup) {
	rg.GET("/governance/settings", governance.GetGovernanceSettings)
	rg.PUT("/governance/settings", governance.UpdateGovernanceSettings)
	rg.GET("/governance/approvals", governance.GetGovernanceApprovals)
	rg.POST("/governance/approvals", governance.CreateGovernanceApproval)
	rg.POST("/governance/approvals/:id/review", governance.ReviewGovernanceApproval)
	rg.DELETE("/governance/approvals/:id", governance.DeleteGovernanceApproval)
	rg.GET("/governance/audit", governance.GetGovernanceAudit)
}

func registerIdentityRoutes(rg *gin.RouterGroup) {
	rg.GET("/identity/config", identity.GetIdpConfig)
	rg.PUT("/identity/config", identity.UpdateIdpConfig)
	rg.GET("/identity/connections", identity.GetIdpConnections)
	rg.POST("/identity/connections", identity.CreateIdpConnection)
	rg.DELETE("/identity/connections/:id", identity.DeleteIdpConnection)
	rg.GET("/identity/mappings", identity.GetGroupMappings)
	rg.POST("/identity/mappings", identity.CreateGroupMapping)
	rg.PUT("/identity/mappings/:id", identity.UpdateGroupMapping)
	rg.DELETE("/identity/mappings/:id", identity.DeleteGroupMapping)
	rg.POST("/identity/mappings/sync", identity.SyncGroupMappings)
	rg.GET("/identity/sessions", identity.GetIdpSessions)
	rg.DELETE("/identity/sessions/:id", identity.RevokeIdpSession)
	rg.DELETE("/identity/sessions", identity.RevokeAllIdpSessions)
	rg.GET("/identity/audit", identity.GetIdpAudit)
	rg.POST("/identity/test/:protocol", identity.TestIdpConnection)
}

func registerAuditRoutes(rg *gin.RouterGroup) {
	rg.GET("/audit", audit.GetAuditEvents)
}

func registerOperationsRoutes(rg *gin.RouterGroup) {
	rg.GET("/operations", operations.GetOperations)
	rg.GET("/operations/:id", operations.GetOperation)
	rg.POST("/operations/:id/status", operations.UpdateOperationStatus)
}
