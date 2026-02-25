package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	authmodels "releaseaapi/internal/features/auth/models"
	platformauth "releaseaapi/internal/platform/auth"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func Login(c *gin.Context) {
	var body authmodels.UserAuthPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.Password = strings.TrimSpace(body.Password)
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	user, err := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"email": body.Email})
	if err != nil {
		shared.RespondError(c, http.StatusUnauthorized, "User not found")
		return
	}

	storedPassword, _ := user["password"].(string)
	if !platformauth.VerifyPassword(body.Password, storedPassword) {
		shared.RespondError(c, http.StatusUnauthorized, "Invalid password")
		return
	}

	userResponse := sanitizeUser(user)
	token, refreshToken, _, err := platformauth.IssueSessionTokens(ctx, user, platformauth.SessionMeta{
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to issue token")
		return
	}
	platformauth.SetRefreshCookie(c, refreshToken)
	platformauth.EnsureCSRFToken(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "auth.login",
		ResourceType: "user",
		ResourceID:   shared.StringValue(user["id"]),
		ActorID:      shared.StringValue(user["id"]),
		ActorName:    shared.StringValue(user["email"]),
		ActorRole:    shared.StringValue(user["role"]),
		Source:       "auth",
	})
	c.JSON(http.StatusOK, gin.H{"user": userResponse, "token": token})
}

func Signup(c *gin.Context) {
	if !shared.EnvBool("ALLOW_USER_SIGNUP", false) {
		shared.RespondError(c, http.StatusForbidden, "User self-signup is disabled")
		return
	}

	var body authmodels.UserAuthPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.Password = strings.TrimSpace(body.Password)
	if body.Name == "" || body.Email == "" || body.Password == "" {
		shared.RespondError(c, http.StatusBadRequest, "Missing required fields")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	_, err := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"email": body.Email})
	if err == nil {
		shared.RespondError(c, http.StatusConflict, "Email already registered")
		return
	}

	team, _ := shared.FindOne(ctx, shared.Collection(shared.TeamsCollection), bson.M{})
	teamID, _ := team["id"].(string)
	teamName, _ := team["name"].(string)
	hashedPassword, err := platformauth.HashPassword(body.Password)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to secure password")
		return
	}
	userID := "user-" + uuid.NewString()
	userDoc := bson.M{
		"_id":      userID,
		"id":       userID,
		"name":     body.Name,
		"email":    body.Email,
		"role":     "developer",
		"teamId":   teamID,
		"teamName": teamName,
		"avatar":   "",
		"password": hashedPassword,
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.UsersCollection), userDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create user")
		return
	}

	profileDoc := bson.M{
		"_id":                userID,
		"id":                 userID,
		"name":               body.Name,
		"email":              body.Email,
		"role":               "developer",
		"teamId":             teamID,
		"teamName":           teamName,
		"twoFactorEnabled":   false,
		"connectedProviders": []interface{}{},
		"sessions":           []interface{}{},
	}
	_ = shared.InsertOne(ctx, shared.Collection(shared.ProfileCollection), profileDoc)

	userResponse := sanitizeUser(userDoc)
	token, refreshToken, _, err := platformauth.IssueSessionTokens(ctx, userDoc, platformauth.SessionMeta{
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to issue token")
		return
	}
	platformauth.SetRefreshCookie(c, refreshToken)
	platformauth.EnsureCSRFToken(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "auth.signup",
		ResourceType: "user",
		ResourceID:   userID,
		ActorID:      userID,
		ActorName:    body.Email,
		ActorRole:    "developer",
		Source:       "auth",
	})
	c.JSON(http.StatusOK, gin.H{"user": userResponse, "token": token})
}

func Logout(c *gin.Context) {
	refreshToken := platformauth.ReadRefreshCookieToken(c)

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	if refreshToken != "" {
		_ = platformauth.RevokeSessionToken(ctx, refreshToken)
	}
	platformauth.ClearRefreshCookie(c)
	platformauth.ClearCSRFCookie(c)

	if bearer := extractBearer(c.GetHeader("Authorization")); bearer != "" {
		if claims, err := platformauth.ParseAuthToken(bearer); err == nil {
			_ = platformauth.RevokeUserSessions(ctx, claims.UserID)
			_, _ = shared.Collection(shared.IdpSessionsCollection).UpdateMany(ctx, bson.M{
				"userId": claims.UserID,
				"active": true,
			}, bson.M{
				"$set": bson.M{
					"active":       false,
					"lastActivity": shared.NowISO(),
				},
			})
			shared.RecordAuditEvent(ctx, shared.AuditEvent{
				Action:       "auth.logout",
				ResourceType: "user",
				ResourceID:   claims.UserID,
				ActorID:      claims.UserID,
				ActorName:    claims.Email,
				ActorRole:    claims.Role,
				Source:       "auth",
			})
		}
	}
	c.Status(http.StatusNoContent)
}

func Refresh(c *gin.Context) {
	refreshToken := platformauth.ReadRefreshCookieToken(c)
	if refreshToken == "" {
		shared.RespondError(c, http.StatusBadRequest, "Refresh token required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	token, nextRefresh, user, err := platformauth.RefreshSessionTokens(ctx, refreshToken, platformauth.SessionMeta{
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})
	if err != nil {
		platformauth.ClearRefreshCookie(c)
		platformauth.ClearCSRFCookie(c)
		shared.RespondError(c, http.StatusUnauthorized, "Invalid refresh token")
		return
	}
	platformauth.SetRefreshCookie(c, nextRefresh)
	platformauth.EnsureCSRFToken(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "auth.refresh",
		ResourceType: "user",
		ResourceID:   shared.StringValue(user["id"]),
		ActorID:      shared.StringValue(user["id"]),
		ActorName:    shared.StringValue(user["email"]),
		ActorRole:    shared.StringValue(user["role"]),
		Source:       "auth",
	})
	c.JSON(http.StatusOK, gin.H{
		"user":  sanitizeUser(user),
		"token": token,
	})
}

func CSRFToken(c *gin.Context) {
	token := platformauth.EnsureCSRFToken(c)
	if token == "" {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to issue CSRF token")
		return
	}
	c.JSON(http.StatusOK, gin.H{"csrfToken": token})
}

func RequestPasswordReset(c *gin.Context) {
	var body authmodels.UserAuthPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if body.Email == "" {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	_, err := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"email": body.Email})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "User not found")
		return
	}

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	resetDoc := bson.M{
		"_id":       token,
		"token":     token,
		"email":     body.Email,
		"expiresAt": expiresAt,
	}
	_ = shared.InsertOne(ctx, shared.Collection(shared.PasswordResetsCollection), resetDoc)
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func ValidatePasswordReset(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		shared.RespondError(c, http.StatusBadRequest, "Token required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	reset, err := shared.FindOne(ctx, shared.Collection(shared.PasswordResetsCollection), bson.M{"token": token})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"valid": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"valid": true, "email": reset["email"]})
}

func ConfirmPasswordReset(c *gin.Context) {
	var body authmodels.PasswordResetConfirmPayload
	if err := c.ShouldBindJSON(&body); err != nil || body.Token == "" {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if body.NewPassword == "" {
		shared.RespondError(c, http.StatusBadRequest, "New password required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	reset, err := shared.FindOne(ctx, shared.Collection(shared.PasswordResetsCollection), bson.M{"token": body.Token})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Invalid token")
		return
	}

	email, _ := reset["email"].(string)
	if email == "" {
		shared.RespondError(c, http.StatusNotFound, "Invalid token")
		return
	}

	userID := findUserIDByEmail(ctx, email)
	if userID == "" {
		shared.RespondError(c, http.StatusNotFound, "User not found")
		return
	}

	hashedPassword, err := platformauth.HashPassword(body.NewPassword)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to secure password")
		return
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.UsersCollection), userID, bson.M{"password": hashedPassword}); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to reset password")
		return
	}
	_ = shared.DeleteByID(ctx, shared.Collection(shared.PasswordResetsCollection), body.Token)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func sanitizeUser(user bson.M) bson.M {
	delete(user, "password")
	return user
}

func findUserIDByEmail(ctx context.Context, email string) string {
	user, err := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"email": email})
	if err != nil {
		return ""
	}
	id, _ := user["_id"].(string)
	return id
}

func extractBearer(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
