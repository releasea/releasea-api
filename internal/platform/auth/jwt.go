package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	httpheaders "releaseaapi/internal/platform/http/headers"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/crypto/bcrypt"
	"releaseaapi/internal/platform/shared"
)

type AuthClaims struct {
	UserID   string `json:"sub"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	TeamID   string `json:"teamId"`
	TeamName string `json:"teamName"`
	jwt.RegisteredClaims
}

type RefreshClaims struct {
	UserID    string `json:"sub"`
	SessionID string `json:"sid"`
	TokenType string `json:"typ"`
	jwt.RegisteredClaims
}

type WorkerClaims struct {
	RegistrationID  string   `json:"regId"`
	Role            string   `json:"role"`
	Name            string   `json:"name"`
	Environment     string   `json:"environment"`
	NamespacePrefix string   `json:"namespacePrefix"`
	Cluster         string   `json:"cluster"`
	Tags            []string `json:"tags"`
	jwt.RegisteredClaims
}

var (
	jwtOnce         sync.Once
	jwtSecret       []byte
	jwtTTL          time.Duration
	refreshJWTOnce  sync.Once
	refreshSecret   []byte
	refreshJWTTTL   time.Duration
	workerJWTOnce   sync.Once
	workerJWTSecret []byte
	workerJWTTTL    time.Duration
)

func getJWTConfig() ([]byte, time.Duration) {
	jwtOnce.Do(func() {
		secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
		if secret == "" {
			secret = "dev-secret-change-me"
			log.Println("[auth] JWT_SECRET not set; using dev default")
		}
		jwtSecret = []byte(secret)

		ttlMinutes := 720
		if rawTTL := strings.TrimSpace(os.Getenv("JWT_TTL_MINUTES")); rawTTL != "" {
			if parsed, err := strconv.Atoi(rawTTL); err == nil && parsed > 0 {
				ttlMinutes = parsed
			}
		}
		jwtTTL = time.Duration(ttlMinutes) * time.Minute
	})

	return jwtSecret, jwtTTL
}

func getWorkerJWTConfig() ([]byte, time.Duration) {
	workerJWTOnce.Do(func() {
		secret := strings.TrimSpace(os.Getenv("WORKER_JWT_SECRET"))
		if secret == "" {
			secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
			if secret == "" {
				secret = "dev-worker-secret-change-me"
			}
			log.Println("[auth] WORKER_JWT_SECRET not set; falling back to JWT_SECRET")
		}
		workerJWTSecret = []byte(secret)

		ttlMinutes := 30
		if rawTTL := strings.TrimSpace(os.Getenv("WORKER_JWT_TTL_MINUTES")); rawTTL != "" {
			if parsed, err := strconv.Atoi(rawTTL); err == nil && parsed > 0 {
				ttlMinutes = parsed
			}
		}
		workerJWTTTL = time.Duration(ttlMinutes) * time.Minute
	})

	return workerJWTSecret, workerJWTTTL
}

func getRefreshJWTConfig() ([]byte, time.Duration) {
	refreshJWTOnce.Do(func() {
		secret := strings.TrimSpace(os.Getenv("JWT_REFRESH_SECRET"))
		if secret == "" {
			secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
			if secret == "" {
				secret = "dev-refresh-secret-change-me"
			}
		}
		refreshSecret = []byte(secret)

		ttlHours := 168
		if rawTTL := strings.TrimSpace(os.Getenv("JWT_REFRESH_TTL_HOURS")); rawTTL != "" {
			if parsed, err := strconv.Atoi(rawTTL); err == nil && parsed > 0 {
				ttlHours = parsed
			}
		}
		refreshJWTTTL = time.Duration(ttlHours) * time.Hour
	})
	return refreshSecret, refreshJWTTTL
}

func GenerateToken(user bson.M) (string, error) {
	secret, ttl := getJWTConfig()

	now := time.Now()
	claims := buildAuthClaims(user, now, ttl)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func buildAuthClaims(user bson.M, now time.Time, ttl time.Duration) AuthClaims {
	userID := shared.StringValue(user["id"])
	if userID == "" {
		userID = shared.StringValue(user["_id"])
	}
	return AuthClaims{
		UserID:   userID,
		Email:    shared.StringValue(user["email"]),
		Role:     shared.StringValue(user["role"]),
		Name:     shared.StringValue(user["name"]),
		TeamID:   shared.StringValue(user["teamId"]),
		TeamName: shared.StringValue(user["teamName"]),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
}

func GenerateRefreshToken(userID, sessionID string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("refresh token requires user and session")
	}
	secret, ttl := getRefreshJWTConfig()
	now := time.Now()
	claims := RefreshClaims{
		UserID:    userID,
		SessionID: sessionID,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func ParseAuthToken(tokenString string) (*AuthClaims, error) {
	secret, _ := getJWTConfig()
	token, err := jwt.ParseWithClaims(tokenString, &AuthClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid auth token")
	}
	claims, ok := token.Claims.(*AuthClaims)
	if !ok {
		return nil, fmt.Errorf("invalid auth claims")
	}
	return claims, nil
}

func ParseRefreshToken(tokenString string) (*RefreshClaims, error) {
	secret, _ := getRefreshJWTConfig()
	token, err := jwt.ParseWithClaims(tokenString, &RefreshClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid refresh token")
	}
	claims, ok := token.Claims.(*RefreshClaims)
	if !ok {
		return nil, fmt.Errorf("invalid refresh claims")
	}
	if claims.TokenType != "refresh" {
		return nil, fmt.Errorf("invalid token type")
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		return nil, fmt.Errorf("missing session id")
	}
	return claims, nil
}

func GenerateWorkerAccessToken(registration bson.M) (string, time.Duration, error) {
	secret, ttl := getWorkerJWTConfig()

	regID := shared.StringValue(registration["id"])
	if regID == "" {
		regID = shared.StringValue(registration["_id"])
	}
	now := time.Now()
	claims := WorkerClaims{
		RegistrationID:  regID,
		Role:            "worker",
		Name:            shared.StringValue(registration["name"]),
		Environment:     shared.StringValue(registration["environment"]),
		NamespacePrefix: shared.StringValue(registration["namespacePrefix"]),
		Cluster:         shared.StringValue(registration["cluster"]),
		Tags:            shared.ToStringSlice(registration["tags"]),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   regID,
			Audience:  []string{"releasea-worker"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", 0, err
	}
	return signed, ttl, nil
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		rawAuthHeader := c.GetHeader(httpheaders.HeaderAuthorization)
		bearerToken, ok := httpheaders.ExtractBearerToken(rawAuthHeader)
		if !ok {
			message := "Invalid authorization header"
			if strings.TrimSpace(rawAuthHeader) == "" {
				message = "Missing authorization token"
			}
			shared.RespondError(c, http.StatusUnauthorized, message)
			c.Abort()
			return
		}

		if isWorkerToken(bearerToken) {
			if !bootstrapTokenAllowed(c.Request.Method, c.Request.URL.Path) {
				shared.RespondError(c, http.StatusForbidden, "Worker token not allowed for this action")
				c.Abort()
				return
			}
			registration, err := validateWorkerToken(bearerToken)
			if err != nil {
				shared.RespondError(c, http.StatusUnauthorized, "Invalid or expired worker token")
				c.Abort()
				return
			}
			c.Set("authRole", "worker-bootstrap")
			c.Set("authName", shared.StringValue(registration["name"]))
			c.Set("authWorkerRegistration", registration)
			c.Next()
			return
		}

		if claims, err := parseWorkerAccessToken(bearerToken); err == nil && claims != nil {
			if !workerJWTAllowed(c.Request.Method, c.Request.URL.Path) {
				shared.RespondError(c, http.StatusForbidden, "Worker token not allowed for this action")
				c.Abort()
				return
			}
			registration, err := loadWorkerRegistration(claims.RegistrationID)
			if err != nil {
				shared.RespondError(c, http.StatusUnauthorized, "Worker registration invalid")
				c.Abort()
				return
			}
			c.Set("authRole", "worker")
			c.Set("authName", claims.Name)
			c.Set("authWorkerRegistration", registration)
			c.Next()
			return
		}

		secret, _ := getJWTConfig()
		token, err := jwt.ParseWithClaims(bearerToken, &AuthClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return secret, nil
		})
		if err != nil || !token.Valid {
			shared.RespondError(c, http.StatusUnauthorized, "Invalid or expired token")
			c.Abort()
			return
		}

		claims, ok := token.Claims.(*AuthClaims)
		if !ok {
			shared.RespondError(c, http.StatusUnauthorized, "Invalid token claims")
			c.Abort()
			return
		}

		if isWorkerOnlyPath(c.Request.Method, c.Request.URL.Path) {
			shared.RespondError(c, http.StatusForbidden, "User token not allowed for worker endpoint")
			c.Abort()
			return
		}

		c.Set("authUserId", claims.UserID)
		c.Set("authRole", claims.Role)
		c.Set("authEmail", claims.Email)
		c.Set("authTeamId", claims.TeamID)
		c.Set("authName", claims.Name)
		c.Next()
	}
}

func parseWorkerAccessToken(tokenString string) (*WorkerClaims, error) {
	secret, _ := getWorkerJWTConfig()
	token, err := jwt.ParseWithClaims(tokenString, &WorkerClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return nil, err
	}
	claims, ok := token.Claims.(*WorkerClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	if claims.Role != "worker" {
		return nil, fmt.Errorf("invalid role")
	}
	if claims.RegistrationID == "" {
		return nil, fmt.Errorf("missing registration")
	}
	if !hasAudience(claims.Audience, "releasea-worker") {
		return nil, fmt.Errorf("invalid audience")
	}
	return claims, nil
}

func isWorkerToken(token string) bool {
	return strings.HasPrefix(token, "frg_reg_")
}

func bootstrapTokenAllowed(method, path string) bool {
	return method == http.MethodPost && strings.Contains(path, "/workers/auth")
}

func workerJWTAllowed(method, path string) bool {
	if method == http.MethodPost {
		if strings.Contains(path, "/workers/heartbeat") {
			return true
		}
		if strings.Contains(path, "/workers/autodeploy/lease") {
			return true
		}
		if strings.Contains(path, "/workers/credentials") {
			return true
		}
		if strings.Contains(path, "/workers/builds") {
			return true
		}
		if strings.Contains(path, "/workers/services/") && strings.HasSuffix(path, "/runtime") {
			return true
		}
		if strings.Contains(path, "/workers/services/") && strings.HasSuffix(path, "/blue-green/primary") {
			return true
		}
		if strings.Contains(path, "/services/") && strings.HasSuffix(path, "/deploys") {
			return true
		}
		if strings.Contains(path, "/deploys/") && strings.HasSuffix(path, "/logs") {
			return true
		}
		if strings.Contains(path, "/rules/") && strings.HasSuffix(path, "/logs") {
			return true
		}
		if strings.Contains(path, "/operations/") && strings.HasSuffix(path, "/status") {
			return true
		}
	}
	if method == http.MethodGet {
		if strings.Contains(path, "/operations") && !strings.HasSuffix(path, "/status") {
			return true
		}
		if strings.Contains(path, "/deploys") {
			return true
		}
		if strings.Contains(path, "/services/") || strings.HasSuffix(path, "/services") {
			return true
		}
		if strings.Contains(path, "/scm/github/commits") {
			return true
		}
		if strings.Contains(path, "/rules/") {
			return true
		}
	}
	return false
}

func isWorkerOnlyPath(method, path string) bool {
	if method == http.MethodPost {
		if strings.Contains(path, "/workers/heartbeat") {
			return true
		}
		if strings.Contains(path, "/workers/autodeploy/lease") {
			return true
		}
		if strings.Contains(path, "/workers/credentials") {
			return true
		}
		if strings.Contains(path, "/workers/builds") {
			return true
		}
		if strings.Contains(path, "/workers/services/") && strings.HasSuffix(path, "/runtime") {
			return true
		}
		if strings.Contains(path, "/workers/services/") && strings.HasSuffix(path, "/blue-green/primary") {
			return true
		}
		if strings.Contains(path, "/operations/") && strings.HasSuffix(path, "/status") {
			return true
		}
	}
	return false
}

func loadWorkerRegistration(registrationID string) (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	registration, err := shared.FindOne(ctx, shared.Collection(shared.WorkerRegistrationsCollection), bson.M{"id": registrationID})
	if err != nil {
		return nil, err
	}
	if !isRegistrationActive(registration) {
		return nil, fmt.Errorf("registration inactive")
	}
	return registration, nil
}

func isRegistrationActive(registration bson.M) bool {
	status := strings.ToLower(shared.StringValue(registration["status"]))
	return status != "revoked"
}

func validateWorkerToken(token string) (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	hint := shared.TokenHint(token)
	registration, err := shared.FindOne(ctx, shared.Collection(shared.WorkerRegistrationsCollection), bson.M{"tokenHint": hint})
	if err != nil {
		registration, err = shared.FindOne(ctx, shared.Collection(shared.WorkerRegistrationsCollection), bson.M{"token": token})
	}
	if err != nil {
		return nil, err
	}

	if err := matchWorkerToken(ctx, registration, token); err != nil {
		return nil, err
	}

	status := strings.ToLower(shared.StringValue(registration["status"]))
	if status == "revoked" {
		return nil, fmt.Errorf("token revoked")
	}
	return registration, nil
}

func matchWorkerToken(ctx context.Context, registration bson.M, token string) error {
	tokenHash := shared.StringValue(registration["tokenHash"])
	if tokenHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)); err != nil {
			return fmt.Errorf("invalid token")
		}
		return nil
	}

	legacyToken := shared.StringValue(registration["token"])
	if legacyToken == "" || legacyToken != token {
		return fmt.Errorf("invalid token")
	}

	// Upgrade legacy token to hash on first use.
	hashed, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return nil
	}
	regID := shared.StringValue(registration["id"])
	if regID == "" {
		regID = shared.StringValue(registration["_id"])
	}
	_, _ = shared.Collection(shared.WorkerRegistrationsCollection).UpdateOne(
		ctx,
		bson.M{"id": regID},
		bson.M{
			"$set": bson.M{
				"tokenHash": string(hashed),
				"tokenHint": shared.TokenHint(token),
			},
			"$unset": bson.M{"token": ""},
		},
	)

	return nil
}

func hasAudience(audience jwt.ClaimStrings, expected string) bool {
	for _, value := range audience {
		if value == expected {
			return true
		}
	}
	return false
}
