package services

import (
	"context"
	"strings"

	"releaseaapi/api/v1/shared"
	"releaseaapi/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ensureBootstrapIdentity(cfg *config.Config, strict bool) error {
	if cfg == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	teamID := strings.TrimSpace(cfg.DefaultTeamID)
	if teamID == "" {
		teamID = "team-1"
	}
	teamName := strings.TrimSpace(cfg.DefaultTeamName)
	if teamName == "" {
		teamName = "Platform"
	}
	teamSlug := shared.ToKubeName(teamName)
	if teamSlug == "" {
		teamSlug = "platform"
	}

	adminID := strings.TrimSpace(cfg.DefaultAdminID)
	if adminID == "" {
		adminID = "user-1"
	}
	adminName := strings.TrimSpace(cfg.DefaultAdminName)
	if adminName == "" {
		adminName = "Platform Admin"
	}
	adminEmail := strings.ToLower(strings.TrimSpace(cfg.DefaultAdminEmail))
	if adminEmail == "" {
		adminEmail = "admin@releasea.io"
	}
	adminPassword := strings.TrimSpace(cfg.DefaultAdminPass)
	if adminPassword == "" {
		adminPassword = "releasea"
	}
	adminPasswordHash, err := shared.HashPassword(adminPassword)
	if err != nil {
		return err
	}

	now := shared.NowISO()
	adminMember := bson.M{
		"id":          adminID,
		"name":        adminName,
		"email":       adminEmail,
		"role":        "admin",
		"avatar":      "",
		"idpProvider": "",
	}
	teamUpdate := bson.M{}
	if strict {
		teamSet := bson.M{
			"id":        teamID,
			"name":      teamName,
			"slug":      teamSlug,
			"updatedAt": now,
		}
		if !cfg.KeepAdditionalUsers {
			teamSet["members"] = []interface{}{adminMember}
		}
		teamUpdate["$set"] = teamSet
		teamUpdate["$setOnInsert"] = bson.M{"createdAt": now}
	} else {
		teamUpdate["$setOnInsert"] = bson.M{
			"id":        teamID,
			"name":      teamName,
			"slug":      teamSlug,
			"members":   []interface{}{adminMember},
			"createdAt": now,
			"updatedAt": now,
		}
	}

	_, err = shared.Collection(shared.TeamsCollection).UpdateOne(ctx, bson.M{"_id": teamID}, teamUpdate, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}

	userUpdate := bson.M{}
	if strict {
		userUpdate["$set"] = bson.M{
			"id":        adminID,
			"name":      adminName,
			"email":     adminEmail,
			"role":      "admin",
			"teamId":    teamID,
			"teamName":  teamName,
			"avatar":    "",
			"password":  adminPasswordHash,
			"updatedAt": now,
		}
		userUpdate["$setOnInsert"] = bson.M{"createdAt": now}
	} else {
		userUpdate["$setOnInsert"] = bson.M{
			"id":        adminID,
			"name":      adminName,
			"email":     adminEmail,
			"role":      "admin",
			"teamId":    teamID,
			"teamName":  teamName,
			"avatar":    "",
			"password":  adminPasswordHash,
			"createdAt": now,
			"updatedAt": now,
		}
	}

	_, err = shared.Collection(shared.UsersCollection).UpdateOne(ctx, bson.M{"_id": adminID}, userUpdate, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}

	profileUpdate := bson.M{}
	if strict {
		profileUpdate["$set"] = bson.M{
			"id":                 adminID,
			"name":               adminName,
			"email":              adminEmail,
			"role":               "admin",
			"teamId":             teamID,
			"teamName":           teamName,
			"identityProvider":   "",
			"twoFactorEnabled":   false,
			"connectedProviders": []interface{}{},
			"sessions":           []interface{}{},
			"updatedAt":          now,
		}
		profileUpdate["$setOnInsert"] = bson.M{"createdAt": now}
	} else {
		profileUpdate["$setOnInsert"] = bson.M{
			"id":                 adminID,
			"name":               adminName,
			"email":              adminEmail,
			"role":               "admin",
			"teamId":             teamID,
			"teamName":           teamName,
			"identityProvider":   "",
			"twoFactorEnabled":   false,
			"connectedProviders": []interface{}{},
			"sessions":           []interface{}{},
			"createdAt":          now,
			"updatedAt":          now,
		}
	}

	_, err = shared.Collection(shared.ProfileCollection).UpdateOne(ctx, bson.M{"_id": adminID}, profileUpdate, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}

	if !strict || cfg.KeepAdditionalUsers {
		return nil
	}

	if _, err := shared.Collection(shared.UsersCollection).DeleteMany(ctx, bson.M{"_id": bson.M{"$ne": adminID}}); err != nil {
		return err
	}
	if _, err := shared.Collection(shared.ProfileCollection).DeleteMany(ctx, bson.M{"_id": bson.M{"$ne": adminID}}); err != nil {
		return err
	}
	return nil
}
