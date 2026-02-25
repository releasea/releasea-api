package shared

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	TeamsCollection               = "teams"
	ProjectsCollection            = "projects"
	ServicesCollection            = "services"
	RulesCollection               = "rules"
	DeploysCollection             = "deploys"
	RuleDeploysCollection         = "rule_deploys"
	LogsCollection                = "logs"
	OperationsCollection          = "operations"
	WorkersCollection             = "workers"
	WorkerRegistrationsCollection = "worker_registrations"
	WorkerLeasesCollection        = "worker_leases"
	RegionsCollection             = "regions"
	EnvironmentsCollection        = "environments"
	ExternalEndpointsCollection   = "external_endpoints"
	DeployTemplatesCollection     = "deploy_templates"
	ServiceTemplatesCollection    = "service_templates"
	ProfileCollection             = "profile"
	PlatformSettingsCollection    = "platform_settings"
	UsersCollection               = "users"
	AuthSessionsCollection        = "auth_sessions"
	AuthSSOStatesCollection       = "auth_sso_states"
	AuthSSOTicketsCollection      = "auth_sso_tickets"
	PasswordResetsCollection      = "password_resets"
	ScmCredentialsCollection      = "scm_credentials"
	RegistryCredentialsCollection = "registry_credentials"
	GovernanceSettingsCollection  = "governance_settings"
	GovernanceApprovalsCollection = "governance_approvals"
	GovernanceAuditCollection     = "governance_audit"
	IdpConfigCollection           = "idp_config"
	IdpConnectionsCollection      = "idp_connections"
	IdpMappingsCollection         = "idp_mappings"
	IdpSessionsCollection         = "idp_sessions"
	IdpAuditCollection            = "idp_audit"
	PlatformAuditCollection       = "platform_audit"
	BuildsCollection              = "builds"
	RuntimeProfilesCollection     = "runtime_profiles"
)

func RespondError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"message": msg})
}

func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
