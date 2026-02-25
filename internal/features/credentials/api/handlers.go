package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"

	credentialmodels "releaseaapi/internal/features/credentials/models"
	"releaseaapi/internal/features/deploys/api"
	platformmodels "releaseaapi/internal/platform/models"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetScmCredentials(c *gin.Context) {
	filter := buildCredentialFilter(c)
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.ScmCredentialsCollection), filter)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load SCM credentials")
		return
	}
	for i := range items {
		items[i] = sanitizeScmCredential(items[i])
	}
	c.JSON(http.StatusOK, items)
}

func CreateScmCredential(c *gin.Context) {
	var payload credentialmodels.CredentialPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if strings.TrimSpace(payload.Name) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Credential name required")
		return
	}
	if strings.TrimSpace(payload.Token) == "" && strings.TrimSpace(payload.PrivateKey) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Token or private key required")
		return
	}
	if payload.Provider == "" {
		payload.Provider = "github"
	}
	if payload.AuthType == "" {
		payload.AuthType = "token"
	}
	scope := normalizeScope(payload.Scope)

	id := "scm-cred-" + uuid.NewString()
	now := shared.NowISO()
	doc := bson.M{
		"_id":        id,
		"id":         id,
		"name":       strings.TrimSpace(payload.Name),
		"provider":   payload.Provider,
		"authType":   payload.AuthType,
		"token":      strings.TrimSpace(payload.Token),
		"privateKey": strings.TrimSpace(payload.PrivateKey),
		"scope":      scope,
		"projectId":  strings.TrimSpace(payload.ProjectID),
		"serviceId":  strings.TrimSpace(payload.ServiceID),
		"notes":      strings.TrimSpace(payload.Notes),
		"createdAt":  now,
		"updatedAt":  now,
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.ScmCredentialsCollection), doc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create SCM credential")
		return
	}
	c.JSON(http.StatusOK, sanitizeScmCredential(doc))
}

func UpdateScmCredential(c *gin.Context) {
	credID := c.Param("id")
	if credID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Credential ID required")
		return
	}
	var payload credentialmodels.CredentialPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	update := bson.M{
		"updatedAt": shared.NowISO(),
	}
	if payload.Name != "" {
		update["name"] = payload.Name
	}
	if payload.Provider != "" {
		update["provider"] = payload.Provider
	}
	if payload.AuthType != "" {
		update["authType"] = payload.AuthType
	}
	if payload.Token != "" {
		update["token"] = payload.Token
	}
	if payload.PrivateKey != "" {
		update["privateKey"] = payload.PrivateKey
	}
	if payload.Scope != "" {
		update["scope"] = normalizeScope(payload.Scope)
	}
	if payload.ProjectID != "" {
		update["projectId"] = payload.ProjectID
	}
	if payload.ServiceID != "" {
		update["serviceId"] = payload.ServiceID
	}
	if payload.Notes != "" {
		update["notes"] = payload.Notes
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ScmCredentialsCollection), credID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update SCM credential")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": credID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load SCM credential")
		return
	}
	c.JSON(http.StatusOK, sanitizeScmCredential(updated))
}

func DeleteScmCredential(c *gin.Context) {
	credID := c.Param("id")
	if credID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Credential ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.ScmCredentialsCollection), credID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete SCM credential")
		return
	}
	c.Status(http.StatusNoContent)
}

func GetRegistryCredentials(c *gin.Context) {
	filter := buildCredentialFilter(c)
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.RegistryCredentialsCollection), filter)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load registry credentials")
		return
	}
	for i := range items {
		items[i] = sanitizeRegistryCredential(items[i])
	}
	c.JSON(http.StatusOK, items)
}

func CreateRegistryCredential(c *gin.Context) {
	var payload credentialmodels.CredentialPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if strings.TrimSpace(payload.Name) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Credential name required")
		return
	}
	if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.Password) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Registry username and password required")
		return
	}
	scope := normalizeScope(payload.Scope)
	id := "reg-cred-" + uuid.NewString()
	now := shared.NowISO()
	doc := bson.M{
		"_id":         id,
		"id":          id,
		"name":        strings.TrimSpace(payload.Name),
		"provider":    payload.Provider,
		"registryUrl": strings.TrimSpace(payload.RegistryURL),
		"username":    strings.TrimSpace(payload.Username),
		"password":    strings.TrimSpace(payload.Password),
		"scope":       scope,
		"projectId":   strings.TrimSpace(payload.ProjectID),
		"serviceId":   strings.TrimSpace(payload.ServiceID),
		"notes":       strings.TrimSpace(payload.Notes),
		"createdAt":   now,
		"updatedAt":   now,
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.RegistryCredentialsCollection), doc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create registry credential")
		return
	}
	c.JSON(http.StatusOK, sanitizeRegistryCredential(doc))
}

func UpdateRegistryCredential(c *gin.Context) {
	credID := c.Param("id")
	if credID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Credential ID required")
		return
	}
	var payload credentialmodels.CredentialPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	update := bson.M{
		"updatedAt": shared.NowISO(),
	}
	if payload.Name != "" {
		update["name"] = payload.Name
	}
	if payload.Provider != "" {
		update["provider"] = payload.Provider
	}
	if payload.RegistryURL != "" {
		update["registryUrl"] = payload.RegistryURL
	}
	if payload.Username != "" {
		update["username"] = payload.Username
	}
	if payload.Password != "" {
		update["password"] = payload.Password
	}
	if payload.Scope != "" {
		update["scope"] = normalizeScope(payload.Scope)
	}
	if payload.ProjectID != "" {
		update["projectId"] = payload.ProjectID
	}
	if payload.ServiceID != "" {
		update["serviceId"] = payload.ServiceID
	}
	if payload.Notes != "" {
		update["notes"] = payload.Notes
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.RegistryCredentialsCollection), credID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update registry credential")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.RegistryCredentialsCollection), bson.M{"id": credID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load registry credential")
		return
	}
	c.JSON(http.StatusOK, sanitizeRegistryCredential(updated))
}

func DeleteRegistryCredential(c *gin.Context) {
	credID := c.Param("id")
	if credID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Credential ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.RegistryCredentialsCollection), credID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete registry credential")
		return
	}
	c.Status(http.StatusNoContent)
}

func WorkerCredentials(c *gin.Context) {
	if role, _ := c.Get("authRole"); role != "worker" {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}

	var payload credentialmodels.WorkerCredentialsRequest
	if err := c.ShouldBindJSON(&payload); err != nil || payload.ServiceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": payload.ServiceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}
	serviceModel := platformmodels.ServiceFromBSON(service)
	var projectModel *platformmodels.Project
	if serviceModel.ProjectID != "" {
		project, _ := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"id": serviceModel.ProjectID})
		if project != nil {
			projectValue := platformmodels.ProjectFromBSON(project)
			projectModel = &projectValue
		}
	}

	scmCred, _ := resolveScmCredential(ctx, serviceModel, projectModel)
	regCred, _ := resolveRegistryCredential(ctx, serviceModel, projectModel)
	template, _ := deploys.ResolveDeployTemplate(ctx, service)
	secretProvider, _ := deploys.ResolveSecretProvider(ctx, service)

	logWorkerCredentials(serviceModel.ID, scmCred, regCred)

	c.JSON(http.StatusOK, gin.H{
		"service":        serviceModel.ToWorkerPayload(),
		"scm":            scmCred,
		"registry":       regCred,
		"template":       template,
		"secretProvider": secretProvider,
	})
}

func resolveScmCredential(ctx context.Context, service platformmodels.Service, project *platformmodels.Project) (bson.M, error) {
	if id := strings.TrimSpace(service.SCMCredentialID); id != "" {
		return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
	}
	if project != nil {
		if id := strings.TrimSpace(project.SCMCredentialID); id != "" {
			return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
		}
	}
	return shared.FindLatestPlatformCredential(ctx, shared.ScmCredentialsCollection)
}

func resolveRegistryCredential(ctx context.Context, service platformmodels.Service, project *platformmodels.Project) (bson.M, error) {
	if id := strings.TrimSpace(service.RegistryCredentialID); id != "" {
		return shared.FindOne(ctx, shared.Collection(shared.RegistryCredentialsCollection), bson.M{"id": id})
	}
	if project != nil {
		if id := strings.TrimSpace(project.RegistryCredentialID); id != "" {
			return shared.FindOne(ctx, shared.Collection(shared.RegistryCredentialsCollection), bson.M{"id": id})
		}
	}
	return shared.FindLatestPlatformCredential(ctx, shared.RegistryCredentialsCollection)
}

func buildCredentialFilter(c *gin.Context) bson.M {
	filter := bson.M{}
	if scope := strings.TrimSpace(c.Query("scope")); scope != "" {
		filter["scope"] = scope
	}
	if projectID := strings.TrimSpace(c.Query("projectId")); projectID != "" {
		filter["projectId"] = projectID
	}
	if serviceID := strings.TrimSpace(c.Query("serviceId")); serviceID != "" {
		filter["serviceId"] = serviceID
	}
	return filter
}

func logWorkerCredentials(serviceID string, scmCred bson.M, regCred bson.M) {
	if serviceID == "" {
		serviceID = "unknown"
	}
	scmID := shared.StringValue(scmCred["id"])
	scmProvider := shared.StringValue(scmCred["provider"])
	scmToken := shared.StringValue(scmCred["token"])
	scmPrivateKey := shared.StringValue(scmCred["privateKey"])
	scmHasToken := scmToken != "" || scmPrivateKey != ""

	regID := shared.StringValue(regCred["id"])
	registryURL := shared.StringValue(regCred["registryUrl"])
	registryUser := shared.StringValue(regCred["username"])
	registryPassword := shared.StringValue(regCred["password"])
	registryHasPassword := registryPassword != ""

	log.Printf("[worker-credentials] service=%s scm_id=%s scm_provider=%s scm_token_set=%t registry_id=%s registry=%s registry_user=%s registry_password_set=%t",
		serviceID,
		scmID,
		scmProvider,
		scmHasToken,
		regID,
		registryURL,
		registryUser,
		registryHasPassword,
	)

	if shared.EnvBool("RELEASEA_DEBUG_CREDENTIALS", false) || shared.EnvBool("WORKER_DEBUG_CREDENTIALS", false) {
		scmSecret := scmToken
		if scmSecret == "" {
			scmSecret = scmPrivateKey
		}
		log.Printf("[worker-credentials] service=%s scm_token_len=%d scm_fingerprint=%s registry_password_len=%d registry_fingerprint=%s",
			serviceID,
			len(scmSecret),
			secretFingerprint(scmSecret),
			len(registryPassword),
			secretFingerprint(registryPassword),
		)
	}
}

func secretFingerprint(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:4])
}

func normalizeScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "project", "service":
		return scope
	default:
		return "platform"
	}
}

func sanitizeScmCredential(doc bson.M) bson.M {
	if doc == nil {
		return bson.M{}
	}
	out := bson.M{}
	for k, v := range doc {
		if k == "token" || k == "privateKey" {
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizeRegistryCredential(doc bson.M) bson.M {
	if doc == nil {
		return bson.M{}
	}
	out := bson.M{}
	for k, v := range doc {
		if k == "password" {
			continue
		}
		out[k] = v
	}
	return out
}
