package security

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"releaseaapi/api/v1/shared"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

type SessionMeta struct {
	IP        string
	UserAgent string
}

func IssueSessionTokens(ctx context.Context, user bson.M, meta SessionMeta) (accessToken string, refreshToken string, sessionID string, err error) {
	userID := shared.StringValue(user["id"])
	if userID == "" {
		userID = shared.StringValue(user["_id"])
	}
	if userID == "" {
		return "", "", "", fmt.Errorf("user id missing")
	}

	accessToken, err = GenerateToken(user)
	if err != nil {
		return "", "", "", err
	}
	sessionID = "sess-" + uuid.NewString()
	refreshToken, err = GenerateRefreshToken(userID, sessionID)
	if err != nil {
		return "", "", "", err
	}
	if err := persistSession(ctx, sessionID, userID, refreshToken, meta); err != nil {
		return "", "", "", err
	}
	return accessToken, refreshToken, sessionID, nil
}

func RefreshSessionTokens(ctx context.Context, refreshToken string, meta SessionMeta) (accessToken string, nextRefreshToken string, user bson.M, err error) {
	claims, err := ParseRefreshToken(strings.TrimSpace(refreshToken))
	if err != nil {
		return "", "", nil, err
	}
	session, err := shared.FindOne(ctx, shared.Collection(shared.AuthSessionsCollection), bson.M{"id": claims.SessionID, "userId": claims.UserID})
	if err != nil {
		return "", "", nil, fmt.Errorf("session not found")
	}
	if strings.ToLower(shared.StringValue(session["status"])) != "active" {
		return "", "", nil, fmt.Errorf("session revoked")
	}
	if isSessionExpired(session["expiresAt"]) {
		_ = markSessionStatus(ctx, claims.SessionID, "expired")
		return "", "", nil, fmt.Errorf("session expired")
	}
	storedHash := shared.StringValue(session["refreshHash"])
	if storedHash == "" || !refreshTokenHashMatches(storedHash, refreshToken) {
		_ = markSessionStatus(ctx, claims.SessionID, "revoked")
		return "", "", nil, fmt.Errorf("session token mismatch")
	}

	user, err = shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"id": claims.UserID})
	if err != nil {
		user, err = shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"_id": claims.UserID})
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("user not found")
	}

	_ = markSessionStatus(ctx, claims.SessionID, "rotated")
	accessToken, nextRefreshToken, _, err = IssueSessionTokens(ctx, user, meta)
	if err != nil {
		return "", "", nil, err
	}
	return accessToken, nextRefreshToken, user, nil
}

func RevokeSessionToken(ctx context.Context, refreshToken string) error {
	claims, err := ParseRefreshToken(strings.TrimSpace(refreshToken))
	if err != nil {
		return err
	}
	session, err := shared.FindOne(ctx, shared.Collection(shared.AuthSessionsCollection), bson.M{"id": claims.SessionID, "userId": claims.UserID})
	if err != nil {
		return nil
	}
	storedHash := shared.StringValue(session["refreshHash"])
	if storedHash == "" {
		return markSessionStatus(ctx, claims.SessionID, "revoked")
	}
	if !refreshTokenHashMatches(storedHash, refreshToken) {
		return nil
	}
	return markSessionStatus(ctx, claims.SessionID, "revoked")
}

func RevokeUserSessions(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	now := shared.NowISO()
	_, err := shared.Collection(shared.AuthSessionsCollection).UpdateMany(ctx, bson.M{
		"userId": userID,
		"status": bson.M{"$in": []string{"active", "rotated"}},
	}, bson.M{
		"$set": bson.M{
			"status":    "revoked",
			"updatedAt": now,
		},
	})
	return err
}

func persistSession(ctx context.Context, sessionID, userID, refreshToken string, meta SessionMeta) error {
	_, ttl := getRefreshJWTConfig()
	now := time.Now().UTC()
	doc := bson.M{
		"_id":         sessionID,
		"id":          sessionID,
		"userId":      userID,
		"status":      "active",
		"refreshHash": hashRefreshToken(refreshToken),
		"ip":          strings.TrimSpace(meta.IP),
		"userAgent":   strings.TrimSpace(meta.UserAgent),
		"createdAt":   now.Format(time.RFC3339),
		"updatedAt":   now.Format(time.RFC3339),
		"expiresAt":   now.Add(ttl).Format(time.RFC3339),
		"lastUsedAt":  now.Format(time.RFC3339),
	}
	return shared.InsertOne(ctx, shared.Collection(shared.AuthSessionsCollection), doc)
}

func markSessionStatus(ctx context.Context, sessionID, status string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return shared.UpdateByID(ctx, shared.Collection(shared.AuthSessionsCollection), sessionID, bson.M{
		"status":    status,
		"updatedAt": shared.NowISO(),
	})
}

func isSessionExpired(raw interface{}) bool {
	value := strings.TrimSpace(shared.StringValue(raw))
	if value == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return time.Now().UTC().After(expiresAt)
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func refreshTokenHashMatches(storedHash, token string) bool {
	expected := hashRefreshToken(token)
	if len(storedHash) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(expected)) == 1
}
