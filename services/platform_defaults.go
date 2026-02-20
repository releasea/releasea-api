package services

import (
	"context"
	"log"
	"strings"
	"time"

	"releaseaapi/api/v1/shared"
	"releaseaapi/client"
	"releaseaapi/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const dbTimeout = 8 * time.Second

func seedDefaults(force bool) error {
	if !force {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	db := client.Mongo().Database(client.DBName)
	log.Printf("[setup] dropping database %s", db.Name())
	return db.Drop(ctx)
}

func ensurePlatformDefaults(cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	db := client.Mongo().Database(client.DBName)
	now := shared.NowISO()
	teamID := bootstrapTeamID(cfg)

	if err := insertIfMissing(ctx, db.Collection(shared.ProjectsCollection), "proj-default", bson.M{
		"id":          "proj-default",
		"name":        "Default",
		"slug":        "default",
		"description": "Default project for new services.",
		"teamId":      teamID,
		"services":    []interface{}{},
		"createdAt":   now,
		"updatedAt":   now,
	}); err != nil {
		return err
	}

	for _, env := range []bson.M{
		{"id": "dev", "name": "Development", "description": "Internal testing", "color": "#3b82f6", "isDefault": false, "namespace": "releasea-apps-development", "createdAt": now, "updatedAt": now},
		{"id": "staging", "name": "Staging", "description": "Pre-production validation", "color": "#f59e0b", "isDefault": false, "namespace": "releasea-apps-staging", "createdAt": now, "updatedAt": now},
		{"id": "prod", "name": "Production", "description": "Customer workloads", "color": "#22c55e", "isDefault": true, "namespace": "releasea-apps-production", "createdAt": now, "updatedAt": now},
	} {
		if err := insertIfMissing(ctx, db.Collection(shared.EnvironmentsCollection), shared.StringValue(env["id"]), env); err != nil {
			return err
		}
	}

	if err := insertIfMissing(ctx, db.Collection(shared.PlatformSettingsCollection), "settings-1", bson.M{
		"organization": bson.M{
			"name":   "Releasea",
			"slug":   "releasea",
			"apiUrl": "https://api.releasea.dev",
		},
		"database": bson.M{
			"mongoUri":  "",
			"rabbitUrl": "",
		},
		"identity": bson.M{
			"saml": bson.M{
				"enabled":     false,
				"entityId":    "",
				"ssoUrl":      "",
				"certificate": "",
			},
			"keycloak": bson.M{
				"enabled":      false,
				"url":          "",
				"realm":        "",
				"clientId":     "",
				"clientSecret": "",
			},
		},
		"notifications": bson.M{
			"deploySuccess": true,
			"deployFailed":  true,
			"serviceDown":   true,
			"workerOffline": true,
			"highCpu":       false,
		},
		"security": bson.M{
			"require2fa":  false,
			"ipAllowlist": false,
			"auditLogs":   true,
		},
		"resourceLimits": bson.M{
			"maxServicesPerProject": 50,
			"maxReplicasPerService": 10,
			"maxCpuPerReplica":      "2000m",
			"maxMemoryPerReplica":   "4Gi",
			"defaultReplicas":       1,
			"defaultCpu":            "250m",
			"defaultMemory":         "512Mi",
		},
		"integrations": []interface{}{},
		"secrets": bson.M{
			"defaultProviderId": "",
			"providers":         []interface{}{},
		},
		"createdAt": now,
		"updatedAt": now,
	}); err != nil {
		return err
	}

	if err := insertIfMissing(ctx, db.Collection(shared.GovernanceSettingsCollection), "gov-settings-1", bson.M{
		"deployApproval": bson.M{
			"enabled":      false,
			"environments": []string{"prod"},
			"minApprovers": 1,
		},
		"rulePublishApproval": bson.M{
			"enabled":      false,
			"externalOnly": false,
			"minApprovers": 1,
		},
		"auditRetentionDays": 30,
		"createdAt":          now,
		"updatedAt":          now,
	}); err != nil {
		return err
	}

	if err := insertIfMissing(ctx, db.Collection(shared.IdpConfigCollection), "idp-config-1", bson.M{
		"saml": bson.M{
			"enabled":                  false,
			"entityId":                 "",
			"ssoUrl":                   "",
			"sloUrl":                   "",
			"certificate":              "",
			"signatureAlgorithm":       "sha256",
			"digestAlgorithm":          "sha256",
			"nameIdFormat":             "emailAddress",
			"assertionEncrypted":       false,
			"wantAuthnRequestsSigned":  true,
			"allowUnsolicitedResponse": false,
			"attributeMapping": bson.M{
				"email":     "email",
				"firstName": "first_name",
				"lastName":  "last_name",
				"groups":    "groups",
			},
		},
		"oidc": bson.M{
			"enabled":           false,
			"issuer":            "",
			"clientId":          "",
			"clientSecret":      "",
			"scopes":            []string{"openid", "profile", "email"},
			"responseType":      "code",
			"tokenEndpointAuth": "client_secret_post",
			"userinfoEndpoint":  "",
			"jwksUri":           "",
			"attributeMapping": bson.M{
				"email":     "email",
				"firstName": "given_name",
				"lastName":  "family_name",
				"groups":    "groups",
			},
		},
		"provisioning": bson.M{
			"autoProvision":         false,
			"autoDeprovision":       false,
			"syncInterval":          60,
			"defaultRole":           "developer",
			"createTeamsFromGroups": false,
		},
		"session": bson.M{
			"maxAge":       86400,
			"idleTimeout":  3600,
			"singleLogout": true,
			"forceReauth":  false,
		},
		"security": bson.M{
			"requireMfa":     false,
			"allowedDomains": []interface{}{},
			"blockedDomains": []interface{}{},
			"ipRestrictions": []interface{}{},
		},
		"createdAt": now,
		"updatedAt": now,
	}); err != nil {
		return err
	}

	for _, profile := range []bson.M{
		{"id": "rp-small", "name": "small", "description": "Lightweight services and sidecars", "cpu": "250m", "cpuLimit": "500m", "memory": "256Mi", "memoryLimit": "512Mi", "createdAt": now, "updatedAt": now},
		{"id": "rp-medium", "name": "medium", "description": "General-purpose APIs and microservices", "cpu": "500m", "cpuLimit": "1000m", "memory": "512Mi", "memoryLimit": "1024Mi", "createdAt": now, "updatedAt": now},
		{"id": "rp-large", "name": "large", "description": "Workers and throughput-oriented services", "cpu": "1000m", "cpuLimit": "2000m", "memory": "1024Mi", "memoryLimit": "2048Mi", "createdAt": now, "updatedAt": now},
	} {
		if err := insertIfMissing(ctx, db.Collection(shared.RuntimeProfilesCollection), shared.StringValue(profile["id"]), profile); err != nil {
			return err
		}
	}

	return nil
}

func insertIfMissing(ctx context.Context, collection *mongo.Collection, id string, doc bson.M) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	doc["_id"] = id
	if _, ok := doc["id"]; !ok {
		doc["id"] = id
	}
	_, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$setOnInsert": doc},
		options.Update().SetUpsert(true),
	)
	return err
}

func bootstrapTeamID(cfg *config.Config) string {
	if cfg != nil {
		teamID := strings.TrimSpace(cfg.DefaultTeamID)
		if teamID != "" {
			return teamID
		}
	}
	return "team-1"
}
