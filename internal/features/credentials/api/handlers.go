package credentials

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"

	credentialmodels "releaseaapi/internal/features/credentials/models"
	deploys "releaseaapi/internal/features/deploys/api"
	platformmodels "releaseaapi/internal/platform/models"
	registryproviders "releaseaapi/internal/platform/providers/registry"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
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
	if payload.AuthType == "" {
		payload.AuthType = "token"
	}
	payload.Provider = scmproviders.Normalize(payload.Provider)
	payload.AuthType = strings.ToLower(strings.TrimSpace(payload.AuthType))
	if err := scmproviders.ValidateCredential(payload.Provider, payload.AuthType); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
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

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	existing, err := shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": credID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "SCM credential not found")
		return
	}
	nextProvider := scmproviders.Normalize(firstNonEmpty(payload.Provider, shared.StringValue(existing["provider"])))
	nextAuthType := strings.ToLower(strings.TrimSpace(firstNonEmpty(payload.AuthType, shared.StringValue(existing["authType"]))))
	if nextAuthType == "" {
		nextAuthType = "token"
	}
	if err := scmproviders.ValidateCredential(nextProvider, nextAuthType); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	update := bson.M{
		"updatedAt": shared.NowISO(),
	}
	if payload.Name != "" {
		update["name"] = payload.Name
	}
	if payload.Provider != "" {
		update["provider"] = nextProvider
	}
	if payload.AuthType != "" {
		update["authType"] = nextAuthType
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
	deleted, err := deleteCredentialByIDOrLegacyObjectID(ctx, shared.Collection(shared.ScmCredentialsCollection), credID)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete SCM credential")
		return
	}
	if !deleted {
		shared.RespondError(c, http.StatusNotFound, "SCM credential not found")
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
	payload.Provider = registryproviders.Normalize(payload.Provider)
	if err := registryproviders.ValidateCredential(payload.Provider); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
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

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	existing, err := shared.FindOne(ctx, shared.Collection(shared.RegistryCredentialsCollection), bson.M{"id": credID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Registry credential not found")
		return
	}
	nextProvider := registryproviders.Normalize(firstNonEmpty(payload.Provider, shared.StringValue(existing["provider"])))
	if err := registryproviders.ValidateCredential(nextProvider); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	update := bson.M{
		"updatedAt": shared.NowISO(),
	}
	if payload.Name != "" {
		update["name"] = payload.Name
	}
	if payload.Provider != "" {
		update["provider"] = nextProvider
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
	deleted, err := deleteCredentialByIDOrLegacyObjectID(ctx, shared.Collection(shared.RegistryCredentialsCollection), credID)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete registry credential")
		return
	}
	if !deleted {
		shared.RespondError(c, http.StatusNotFound, "Registry credential not found")
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

	scmCred, scmErr := resolveScmCredential(ctx, serviceModel, projectModel)
	if scmErr != nil {
		log.Printf("[worker-credentials] service=%s failed to resolve scm credential: %v", serviceModel.ID, scmErr)
		scmCred = bson.M{}
	}
	regCred, regErr := resolveRegistryCredential(ctx, serviceModel, projectModel)
	if regErr != nil {
		log.Printf("[worker-credentials] service=%s failed to resolve registry credential: %v", serviceModel.ID, regErr)
		regCred = bson.M{}
	}
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
		cred, found, err := resolveCredentialByIDOrLegacyObjectID(ctx, shared.ScmCredentialsCollection, id)
		if err != nil {
			return nil, err
		}
		if found {
			return cred, nil
		}
		log.Printf("[worker-credentials] service=%s missing scm credential id=%s; falling back", service.ID, id)
	}
	if project != nil {
		if id := strings.TrimSpace(project.SCMCredentialID); id != "" {
			cred, found, err := resolveCredentialByIDOrLegacyObjectID(ctx, shared.ScmCredentialsCollection, id)
			if err != nil {
				return nil, err
			}
			if found {
				return cred, nil
			}
			log.Printf("[worker-credentials] project=%s missing scm credential id=%s; falling back", project.ID, id)
		}
	}
	cred, err := shared.FindLatestPlatformCredential(ctx, shared.ScmCredentialsCollection)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return bson.M{}, nil
	}
	return cred, err
}

func resolveRegistryCredential(ctx context.Context, service platformmodels.Service, project *platformmodels.Project) (bson.M, error) {
	if id := strings.TrimSpace(service.RegistryCredentialID); id != "" {
		cred, found, err := resolveCredentialByIDOrLegacyObjectID(ctx, shared.RegistryCredentialsCollection, id)
		if err != nil {
			return nil, err
		}
		if found {
			return cred, nil
		}
		log.Printf("[worker-credentials] service=%s missing registry credential id=%s; falling back", service.ID, id)
	}
	if project != nil {
		if id := strings.TrimSpace(project.RegistryCredentialID); id != "" {
			cred, found, err := resolveCredentialByIDOrLegacyObjectID(ctx, shared.RegistryCredentialsCollection, id)
			if err != nil {
				return nil, err
			}
			if found {
				return cred, nil
			}
			log.Printf("[worker-credentials] project=%s missing registry credential id=%s; falling back", project.ID, id)
		}
	}
	cred, err := shared.FindLatestPlatformCredential(ctx, shared.RegistryCredentialsCollection)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return bson.M{}, nil
	}
	return cred, err
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
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
	ensureCredentialDocumentID(out)
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
	ensureCredentialDocumentID(out)
	return out
}

func deleteCredentialByIDOrLegacyObjectID(ctx context.Context, col *mongo.Collection, credentialID string) (bool, error) {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return false, nil
	}

	orFilters := []bson.M{
		{"id": credentialID},
		{"_id": credentialID},
	}
	if objectID, err := primitive.ObjectIDFromHex(credentialID); err == nil {
		orFilters = append(orFilters, bson.M{"_id": objectID})
	}

	result, err := col.DeleteOne(ctx, bson.M{"$or": orFilters})
	if err != nil {
		return false, err
	}
	return result.DeletedCount > 0, nil
}

func ensureCredentialDocumentID(doc bson.M) {
	if doc == nil {
		return
	}
	if strings.TrimSpace(shared.StringValue(doc["id"])) != "" {
		return
	}

	switch value := doc["_id"].(type) {
	case string:
		doc["id"] = strings.TrimSpace(value)
	case primitive.ObjectID:
		doc["id"] = value.Hex()
	case bson.M:
		if oid, ok := value["$oid"].(string); ok {
			doc["id"] = strings.TrimSpace(oid)
		}
	case map[string]interface{}:
		if oid, ok := value["$oid"].(string); ok {
			doc["id"] = strings.TrimSpace(oid)
		}
	}
}

func resolveCredentialByIDOrLegacyObjectID(ctx context.Context, collectionName, credentialID string) (bson.M, bool, error) {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return nil, false, nil
	}

	orFilters := []bson.M{
		{"id": credentialID},
		{"_id": credentialID},
	}
	if objectID, err := primitive.ObjectIDFromHex(credentialID); err == nil {
		orFilters = append(orFilters, bson.M{"_id": objectID})
	}

	doc, err := shared.FindOne(ctx, shared.Collection(collectionName), bson.M{"$or": orFilters})
	if err == nil {
		return doc, true, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	return nil, false, err
}
