package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"releaseaapi/api/v1/security"
	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	ssoStateTTL  = 10 * time.Minute
	ssoTicketTTL = 2 * time.Minute
)

type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func GetSSOConfig(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	_, cfg, err := loadIDPConfig(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "provider": ""})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": cfg.OIDC.Enabled,
		"provider": func() string {
			if cfg.OIDC.Enabled {
				return "oidc"
			}
			return ""
		}(),
	})
}

func StartSSO(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, cfg, err := loadIDPConfig(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, "SSO is not configured")
		return
	}
	if !cfg.OIDC.Enabled {
		shared.RespondError(c, http.StatusBadRequest, "OIDC SSO is not enabled")
		return
	}
	if cfg.OIDC.TokenEndpointAuth == "private_key_jwt" {
		shared.RespondError(c, http.StatusBadRequest, "OIDC tokenEndpointAuth private_key_jwt is not supported in login flow")
		return
	}

	discovery, err := resolveOIDCDiscovery(ctx, cfg.OIDC)
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Unable to resolve OIDC discovery")
		return
	}

	appRedirect, err := resolveSSORedirectURL(c)
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	callbackURL := resolveSSOCallbackURL(c)
	state, err := generateSecureToken(32)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to initialize SSO state")
		return
	}
	fromPath := sanitizeInternalPath(c.Query("from"))

	now := time.Now().UTC()
	stateDoc := bson.M{
		"_id":              state,
		"state":            state,
		"provider":         "oidc",
		"redirectUri":      appRedirect,
		"from":             fromPath,
		"callbackUri":      callbackURL,
		"tokenEndpoint":    discovery.TokenEndpoint,
		"userinfoEndpoint": pickUserInfoEndpoint(cfg.OIDC.UserinfoEndpoint, discovery.UserinfoEndpoint),
		"issuer":           pickIssuer(cfg.OIDC.Issuer, discovery.Issuer),
		"createdAt":        now.Format(time.RFC3339),
		"expiresAt":        now.Add(ssoStateTTL).Format(time.RFC3339),
		"used":             false,
		"ipAddress":        strings.TrimSpace(c.ClientIP()),
		"userAgent":        strings.TrimSpace(c.GetHeader("User-Agent")),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.AuthSSOStatesCollection), stateDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to persist SSO state")
		return
	}

	authURL, err := buildOIDCAuthorizationURL(discovery.AuthorizationEndpoint, cfg.OIDC, callbackURL, state, cfg.Session.ForceReauth)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to build SSO authorization URL")
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

func CompleteSSO(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		redirectWithSSOError(c, resolveDefaultAuthRedirect(c), "missing_sso_state")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	stateDoc, err := consumeSSOState(ctx, state)
	if err != nil {
		redirectWithSSOError(c, resolveDefaultAuthRedirect(c), "invalid_or_expired_sso_state")
		return
	}
	redirectURI := shared.StringValue(stateDoc["redirectUri"])
	if redirectURI == "" {
		redirectURI = resolveDefaultAuthRedirect(c)
	}

	if oidcErr := strings.TrimSpace(c.Query("error")); oidcErr != "" {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "OIDC provider denied authentication")
		redirectWithSSOError(c, redirectURI, "sso_access_denied")
		return
	}

	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "OIDC callback did not return code")
		redirectWithSSOError(c, redirectURI, "missing_authorization_code")
		return
	}

	_, cfg, err := loadIDPConfig(ctx)
	if err != nil {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "Identity provider config not found")
		redirectWithSSOError(c, redirectURI, "identity_provider_not_configured")
		return
	}
	if !cfg.OIDC.Enabled {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "OIDC SSO disabled during callback")
		redirectWithSSOError(c, redirectURI, "oidc_sso_disabled")
		return
	}

	callbackURL := shared.StringValue(stateDoc["callbackUri"])
	tokenEndpoint := shared.StringValue(stateDoc["tokenEndpoint"])
	if callbackURL == "" || tokenEndpoint == "" {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "SSO state missing token exchange fields")
		redirectWithSSOError(c, redirectURI, "invalid_sso_state")
		return
	}

	tokenResp, err := exchangeOIDCCode(ctx, tokenEndpoint, cfg.OIDC, code, callbackURL)
	if err != nil {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "OIDC token exchange failed")
		redirectWithSSOError(c, redirectURI, "oidc_token_exchange_failed")
		return
	}

	claims, err := resolveOIDCClaims(ctx, tokenResp, shared.StringValue(stateDoc["userinfoEndpoint"]))
	if err != nil {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "OIDC user claims fetch failed")
		redirectWithSSOError(c, redirectURI, "oidc_claims_fetch_failed")
		return
	}

	email, firstName, lastName, groups := extractOIDCIdentity(claims, cfg.OIDC.AttributeMapping)
	if err := enforceSSORestrictions(email, c.ClientIP(), cfg.Security); err != nil {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "OIDC access blocked by security policy")
		redirectWithSSOError(c, redirectURI, "sso_access_restricted")
		return
	}

	user, err := findOrCreateSSOUser(ctx, cfg, email, firstName, lastName, groups)
	if err != nil {
		recordIDPAuditEvent(ctx, "login_failed", nil, "oidc", c.ClientIP(), "Failed to map OIDC identity to local user")
		redirectWithSSOError(c, redirectURI, "sso_user_provisioning_failed")
		return
	}

	token, refreshToken, sessionID, err := security.IssueSessionTokens(ctx, user, security.SessionMeta{
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
	})
	if err != nil {
		recordIDPAuditEvent(ctx, "login_failed", user, "oidc", c.ClientIP(), "Failed to issue platform session")
		redirectWithSSOError(c, redirectURI, "failed_to_issue_session")
		return
	}

	_ = persistIDPSession(ctx, user, sessionID, providerNameFromIssuer(shared.StringValue(stateDoc["issuer"])), "oidc", c.ClientIP(), c.GetHeader("User-Agent"), cfg.Session.MaxAge)
	recordIDPAuditEvent(ctx, "login_success", user, "oidc", c.ClientIP(), "OIDC login completed")

	ticket, err := createSSOTicket(ctx, sanitizeUserDocument(user), token, refreshToken)
	if err != nil {
		redirectWithSSOError(c, redirectURI, "failed_to_finalize_sso")
		return
	}

	redirectTarget := addQueryParam(redirectURI, "ssoTicket", ticket)
	if fromPath := shared.StringValue(stateDoc["from"]); fromPath != "" {
		redirectTarget = addQueryParam(redirectTarget, "from", fromPath)
	}
	c.Redirect(http.StatusFound, redirectTarget)
}

func ExchangeSSOTicket(c *gin.Context) {
	var payload struct {
		Ticket string `json:"ticket"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	payload.Ticket = strings.TrimSpace(payload.Ticket)
	if payload.Ticket == "" {
		shared.RespondError(c, http.StatusBadRequest, "SSO ticket required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	doc, err := consumeSSOTicket(ctx, payload.Ticket)
	if err != nil {
		shared.RespondError(c, http.StatusUnauthorized, "Invalid or expired SSO ticket")
		return
	}

	user := shared.MapPayload(doc["user"])
	token := shared.StringValue(doc["token"])
	refreshToken := shared.StringValue(doc["refreshToken"])
	if len(user) == 0 || token == "" {
		shared.RespondError(c, http.StatusUnauthorized, "Invalid or expired SSO ticket")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user":         user,
		"token":        token,
		"refreshToken": refreshToken,
	})
}

func resolveOIDCDiscovery(ctx context.Context, cfg oidcConfig) (oidcDiscovery, error) {
	raw, err := fetchHTTPBody(ctx, buildOIDCDiscoveryURL(cfg.Issuer))
	if err != nil {
		return oidcDiscovery{}, err
	}
	var discovery oidcDiscovery
	if err := json.Unmarshal(raw, &discovery); err != nil {
		return oidcDiscovery{}, err
	}
	if strings.TrimSpace(discovery.AuthorizationEndpoint) == "" {
		return oidcDiscovery{}, fmt.Errorf("oidc discovery missing authorization_endpoint")
	}
	if strings.TrimSpace(discovery.TokenEndpoint) == "" {
		return oidcDiscovery{}, fmt.Errorf("oidc discovery missing token_endpoint")
	}
	return discovery, nil
}

func buildOIDCAuthorizationURL(authEndpoint string, cfg oidcConfig, callbackURL, state string, forcePrompt bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(authEndpoint))
	if err != nil {
		return "", err
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	if !containsString(scopes, "openid") {
		scopes = append(scopes, "openid")
	}

	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", callbackURL)
	query.Set("scope", strings.Join(scopes, " "))
	query.Set("state", state)
	if forcePrompt {
		query.Set("prompt", "login")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func resolveSSORedirectURL(c *gin.Context) (string, error) {
	redirect := strings.TrimSpace(c.Query("redirect"))
	if redirect == "" {
		redirect = strings.TrimSpace(os.Getenv("AUTH_SSO_REDIRECT_URL"))
	}
	if redirect == "" {
		if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
			redirect = strings.TrimSuffix(origin, "/") + "/auth"
		}
	}
	if redirect == "" {
		redirect = "http://localhost:5173/auth"
	}
	parsed, err := url.Parse(redirect)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("Invalid redirect URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("Invalid redirect URL protocol")
	}
	if !strings.HasPrefix(parsed.Path, "/auth") {
		return "", fmt.Errorf("Redirect path must start with /auth")
	}
	if !isAllowedRedirectOrigin(c, parsed) {
		return "", fmt.Errorf("Redirect URL is not allowed")
	}
	return parsed.String(), nil
}

func isAllowedRedirectOrigin(c *gin.Context, parsed *url.URL) bool {
	raw := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	if raw == "*" {
		return true
	}
	allowed := map[string]struct{}{
		"http://localhost:3000": {},
		"http://localhost:5173": {},
	}
	if raw != "" {
		for _, part := range strings.Split(raw, ",") {
			candidate := strings.TrimSpace(part)
			if candidate == "" {
				continue
			}
			origin, err := normalizeOrigin(candidate)
			if err != nil {
				continue
			}
			allowed[origin] = struct{}{}
		}
	}
	origin := parsed.Scheme + "://" + parsed.Host
	_, ok := allowed[origin]
	return ok
}

func normalizeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid origin")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("invalid origin")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func resolveSSOCallbackURL(c *gin.Context) string {
	if callback := strings.TrimSpace(os.Getenv("AUTH_SSO_CALLBACK_URL")); callback != "" {
		return callback
	}
	baseURL := requestBaseURL(c)
	path := strings.TrimSuffix(c.Request.URL.Path, "/start") + "/callback"
	return strings.TrimSuffix(baseURL, "/") + path
}

func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	} else if c.Request.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	return scheme + "://" + host
}

func resolveDefaultAuthRedirect(c *gin.Context) string {
	if redirect := strings.TrimSpace(os.Getenv("AUTH_SSO_REDIRECT_URL")); redirect != "" {
		return redirect
	}
	if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" {
		return strings.TrimSuffix(origin, "/") + "/auth"
	}
	return "http://localhost:5173/auth"
}

func sanitizeInternalPath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		return ""
	}
	if strings.Contains(value, "\\") {
		return ""
	}
	return value
}

func exchangeOIDCCode(ctx context.Context, endpoint string, cfg oidcConfig, code, callbackURL string) (oidcTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", callbackURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oidcTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	switch cfg.TokenEndpointAuth {
	case "client_secret_basic":
		req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)
	case "", "client_secret_post":
		form.Set("client_id", cfg.ClientID)
		form.Set("client_secret", cfg.ClientSecret)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.ContentLength = int64(len(form.Encode()))
	case "private_key_jwt":
		return oidcTokenResponse{}, fmt.Errorf("private_key_jwt not supported")
	default:
		return oidcTokenResponse{}, fmt.Errorf("unsupported token endpoint auth mode")
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return oidcTokenResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return oidcTokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oidcTokenResponse{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tokenResponse oidcTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return oidcTokenResponse{}, err
	}
	if strings.TrimSpace(tokenResponse.AccessToken) == "" {
		return oidcTokenResponse{}, fmt.Errorf("token response missing access_token")
	}
	return tokenResponse, nil
}

func resolveOIDCClaims(ctx context.Context, tokens oidcTokenResponse, userinfoEndpoint string) (map[string]interface{}, error) {
	endpoint := strings.TrimSpace(userinfoEndpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("userinfo endpoint missing")
	}
	return fetchOIDCUserInfo(ctx, endpoint, tokens.AccessToken)
}

func fetchOIDCUserInfo(ctx context.Context, endpoint, accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("userinfo endpoint returned status %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func extractOIDCIdentity(claims map[string]interface{}, mapping attributeMapping) (email string, firstName string, lastName string, groups []string) {
	email = firstNonEmpty(
		claimString(claims, mapping.Email),
		claimString(claims, "email"),
		claimString(claims, "upn"),
	)
	firstName = firstNonEmpty(
		claimString(claims, mapping.FirstName),
		claimString(claims, "given_name"),
		claimString(claims, "name"),
	)
	lastName = firstNonEmpty(
		claimString(claims, mapping.LastName),
		claimString(claims, "family_name"),
	)
	groups = firstNonEmptyGroupList(
		claimStringSlice(claims, mapping.Groups),
		claimStringSlice(claims, "groups"),
		claimStringSlice(claims, "roles"),
	)
	email = strings.ToLower(strings.TrimSpace(email))
	return email, strings.TrimSpace(firstName), strings.TrimSpace(lastName), groups
}

func claimString(claims map[string]interface{}, path string) string {
	value := claimValue(claims, path)
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func claimStringSlice(claims map[string]interface{}, path string) []string {
	value := claimValue(claims, path)
	switch v := value.(type) {
	case []string:
		return normalizeStringSlice(v)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return normalizeStringSlice(out)
	case string:
		pieces := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == ';' || r == '\t' || r == '\n'
		})
		return normalizeStringSlice(pieces)
	default:
		return nil
	}
}

func claimValue(claims map[string]interface{}, path string) interface{} {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if !strings.Contains(path, ".") {
		return claims[path]
	}
	var current interface{} = claims
	for _, part := range strings.Split(path, ".") {
		segment := strings.TrimSpace(part)
		if segment == "" {
			return nil
		}
		asMap, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = asMap[segment]
	}
	return current
}

func normalizeStringSlice(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		candidate := strings.TrimSpace(value)
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
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

func firstNonEmptyGroupList(candidates ...[]string) []string {
	for _, candidate := range candidates {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

func enforceSSORestrictions(email, ipAddress string, cfg securityConfig) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("email claim missing")
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("email claim invalid")
	}
	domain := strings.ToLower(strings.TrimSpace(parts[1]))

	for _, blocked := range cfg.BlockedDomains {
		if domainMatchesPattern(domain, blocked) {
			return fmt.Errorf("email domain blocked")
		}
	}
	if len(cfg.AllowedDomains) > 0 {
		allowed := false
		for _, candidate := range cfg.AllowedDomains {
			if domainMatchesPattern(domain, candidate) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("email domain is not allowed")
		}
	}
	if len(cfg.IPRestrictions) > 0 && !isIPAllowed(ipAddress, cfg.IPRestrictions) {
		return fmt.Errorf("ip address blocked")
	}
	return nil
}

func domainMatchesPattern(domain, pattern string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if domain == "" || pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*.")
		return domain == suffix || strings.HasSuffix(domain, "."+suffix)
	}
	return domain == pattern
}

func isIPAllowed(ipAddress string, restrictions []string) bool {
	parsedIP := net.ParseIP(strings.TrimSpace(ipAddress))
	if parsedIP == nil {
		return false
	}
	for _, restriction := range restrictions {
		candidate := strings.TrimSpace(restriction)
		if candidate == "" {
			continue
		}
		if ip := net.ParseIP(candidate); ip != nil {
			if ip.Equal(parsedIP) {
				return true
			}
			continue
		}
		if _, network, err := net.ParseCIDR(candidate); err == nil && network.Contains(parsedIP) {
			return true
		}
	}
	return false
}

func findOrCreateSSOUser(ctx context.Context, cfg idpConfig, email, firstName, lastName string, groups []string) (bson.M, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	query := bson.M{"email": bson.M{"$regex": "^" + regexp.QuoteMeta(email) + "$", "$options": "i"}}
	user, err := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), query)
	if err == nil {
		userID := recordID(user)
		if userID != "" {
			updates := bson.M{"idpProvider": "oidc"}
			if fullName := strings.TrimSpace(nameFromClaims(firstName, lastName)); fullName != "" {
				updates["name"] = fullName
			}
			_ = shared.UpdateByID(ctx, shared.Collection(shared.UsersCollection), userID, updates)
			if refreshed, loadErr := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), bson.M{"_id": userID}); loadErr == nil {
				user = refreshed
			}
		}
		_ = ensureProfileForSSOUser(ctx, user, "oidc")
		return user, nil
	}
	if !cfg.Provisioning.AutoProvision {
		return nil, fmt.Errorf("user not found and autoProvision disabled")
	}

	role, teamID, teamName := resolveProvisioningTarget(ctx, cfg, groups)
	userID := "user-" + uuid.NewString()
	name := nameFromClaims(firstName, lastName)
	if name == "" {
		name = email
	}
	userDoc := bson.M{
		"_id":         userID,
		"id":          userID,
		"name":        name,
		"email":       email,
		"role":        role,
		"teamId":      teamID,
		"teamName":    teamName,
		"avatar":      "",
		"password":    "",
		"idpProvider": "oidc",
		"createdAt":   shared.NowISO(),
		"updatedAt":   shared.NowISO(),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.UsersCollection), userDoc); err != nil {
		return nil, err
	}
	_ = ensureProfileForSSOUser(ctx, userDoc, "oidc")
	return userDoc, nil
}

func resolveProvisioningTarget(ctx context.Context, cfg idpConfig, groups []string) (role, teamID, teamName string) {
	role = normalizeRole(cfg.Provisioning.DefaultRole)
	teamID, teamName = defaultTeam(ctx)

	normalizedGroups := normalizeStringSlice(groups)
	if len(normalizedGroups) == 0 {
		return role, teamID, teamName
	}
	mappings, err := shared.FindAll(ctx, shared.Collection(shared.IdpMappingsCollection), bson.M{"syncEnabled": true})
	if err != nil {
		return role, teamID, teamName
	}
	for _, mapping := range mappings {
		external := strings.ToLower(strings.TrimSpace(shared.StringValue(mapping["externalGroup"])))
		if external == "" {
			continue
		}
		for _, group := range normalizedGroups {
			if strings.EqualFold(group, external) {
				if mappedRole := normalizeRole(shared.StringValue(mapping["role"])); mappedRole != "" {
					role = mappedRole
				}
				if mappedTeamID := strings.TrimSpace(shared.StringValue(mapping["internalTeamId"])); mappedTeamID != "" {
					teamID = mappedTeamID
				}
				if mappedTeamName := strings.TrimSpace(shared.StringValue(mapping["internalTeamName"])); mappedTeamName != "" {
					teamName = mappedTeamName
				}
				return role, teamID, teamName
			}
		}
	}
	return role, teamID, teamName
}

func normalizeRole(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "admin":
		return "admin"
	default:
		return "developer"
	}
}

func defaultTeam(ctx context.Context) (teamID, teamName string) {
	team, err := shared.FindOne(ctx, shared.Collection(shared.TeamsCollection), bson.M{})
	if err != nil {
		return "", ""
	}
	teamID = shared.StringValue(team["id"])
	if teamID == "" {
		teamID = shared.StringValue(team["_id"])
	}
	teamName = shared.StringValue(team["name"])
	return teamID, teamName
}

func ensureProfileForSSOUser(ctx context.Context, user bson.M, provider string) error {
	userID := shared.StringValue(user["id"])
	if userID == "" {
		userID = shared.StringValue(user["_id"])
	}
	if userID == "" {
		return fmt.Errorf("missing user id")
	}

	now := shared.NowISO()
	existing, err := shared.FindOne(ctx, shared.Collection(shared.ProfileCollection), bson.M{"_id": userID})
	if err != nil {
		doc := bson.M{
			"_id":                userID,
			"id":                 userID,
			"name":               shared.StringValue(user["name"]),
			"email":              shared.StringValue(user["email"]),
			"role":               shared.StringValue(user["role"]),
			"teamId":             shared.StringValue(user["teamId"]),
			"teamName":           shared.StringValue(user["teamName"]),
			"identityProvider":   provider,
			"twoFactorEnabled":   false,
			"connectedProviders": []interface{}{bson.M{"id": provider, "provider": provider, "connectedAt": now}},
			"sessions":           []interface{}{},
			"createdAt":          now,
			"updatedAt":          now,
		}
		return shared.InsertOne(ctx, shared.Collection(shared.ProfileCollection), doc)
	}

	connected := toInterfaceSlice(existing["connectedProviders"])
	nextConnected := upsertProviderConnection(connected, provider, now)

	updates := bson.M{
		"name":               shared.StringValue(user["name"]),
		"email":              shared.StringValue(user["email"]),
		"role":               shared.StringValue(user["role"]),
		"teamId":             shared.StringValue(user["teamId"]),
		"teamName":           shared.StringValue(user["teamName"]),
		"identityProvider":   provider,
		"connectedProviders": nextConnected,
		"updatedAt":          now,
	}
	return shared.UpdateByID(ctx, shared.Collection(shared.ProfileCollection), userID, updates)
}

func toInterfaceSlice(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	default:
		return nil
	}
}

func upsertProviderConnection(existing []interface{}, provider, now string) []interface{} {
	if len(existing) == 0 {
		return []interface{}{bson.M{"id": provider, "provider": provider, "connectedAt": now}}
	}
	updated := make([]interface{}, 0, len(existing)+1)
	found := false
	for _, item := range existing {
		var entry map[string]interface{}
		switch cast := item.(type) {
		case map[string]interface{}:
			entry = cast
		case bson.M:
			entry = map[string]interface{}(cast)
		default:
			updated = append(updated, item)
			continue
		}
		identifier := shared.StringValue(entry["id"])
		if identifier == "" {
			identifier = shared.StringValue(entry["provider"])
		}
		if strings.EqualFold(identifier, provider) {
			updated = append(updated, bson.M{"id": provider, "provider": provider, "connectedAt": now})
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		updated = append(updated, bson.M{"id": provider, "provider": provider, "connectedAt": now})
	}
	return updated
}

func persistIDPSession(ctx context.Context, user bson.M, authSessionID, providerName, provider, ipAddress, userAgent string, maxAgeSeconds int) error {
	userID := shared.StringValue(user["id"])
	if userID == "" {
		userID = shared.StringValue(user["_id"])
	}
	if userID == "" {
		return fmt.Errorf("missing user id")
	}
	if maxAgeSeconds <= 0 {
		maxAgeSeconds = 86400
	}
	now := time.Now().UTC()
	doc := bson.M{
		"_id":           "idpsess-" + uuid.NewString(),
		"userId":        userID,
		"userName":      shared.StringValue(user["name"]),
		"userEmail":     shared.StringValue(user["email"]),
		"provider":      provider,
		"providerName":  firstNonEmpty(providerName, strings.ToUpper(provider)),
		"createdAt":     now.Format(time.RFC3339),
		"expiresAt":     now.Add(time.Duration(maxAgeSeconds) * time.Second).Format(time.RFC3339),
		"lastActivity":  now.Format(time.RFC3339),
		"ipAddress":     strings.TrimSpace(ipAddress),
		"userAgent":     strings.TrimSpace(userAgent),
		"active":        true,
		"authSessionId": authSessionID,
	}
	return shared.InsertOne(ctx, shared.Collection(shared.IdpSessionsCollection), doc)
}

func recordIDPAuditEvent(ctx context.Context, action string, user bson.M, provider, ipAddress, details string) {
	doc := bson.M{
		"_id":       "idpaudit-" + uuid.NewString(),
		"timestamp": shared.NowISO(),
		"action":    action,
		"provider":  provider,
		"ipAddress": strings.TrimSpace(ipAddress),
		"details":   details,
	}
	if user != nil {
		doc["userId"] = firstNonEmpty(shared.StringValue(user["id"]), shared.StringValue(user["_id"]))
		doc["userName"] = firstNonEmpty(shared.StringValue(user["name"]), shared.StringValue(user["email"]))
	}
	_ = shared.InsertOne(ctx, shared.Collection(shared.IdpAuditCollection), doc)
}

func createSSOTicket(ctx context.Context, user bson.M, token, refreshToken string) (string, error) {
	ticket, err := generateSecureToken(48)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	doc := bson.M{
		"_id":          ticket,
		"ticket":       ticket,
		"user":         user,
		"token":        token,
		"refreshToken": refreshToken,
		"used":         false,
		"createdAt":    now.Format(time.RFC3339),
		"expiresAt":    now.Add(ssoTicketTTL).Format(time.RFC3339),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.AuthSSOTicketsCollection), doc); err != nil {
		return "", err
	}
	return ticket, nil
}

func consumeSSOTicket(ctx context.Context, ticket string) (bson.M, error) {
	doc, err := shared.FindOne(ctx, shared.Collection(shared.AuthSSOTicketsCollection), bson.M{"_id": ticket})
	if err != nil {
		return nil, err
	}
	if shared.BoolValue(doc["used"]) {
		return nil, fmt.Errorf("ticket already used")
	}
	if isExpired(doc["expiresAt"]) {
		_ = shared.DeleteByID(ctx, shared.Collection(shared.AuthSSOTicketsCollection), ticket)
		return nil, fmt.Errorf("ticket expired")
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.AuthSSOTicketsCollection), ticket, bson.M{"used": true, "usedAt": shared.NowISO()}); err != nil {
		return nil, err
	}
	_ = shared.DeleteByID(ctx, shared.Collection(shared.AuthSSOTicketsCollection), ticket)
	return doc, nil
}

func consumeSSOState(ctx context.Context, state string) (bson.M, error) {
	doc, err := shared.FindOne(ctx, shared.Collection(shared.AuthSSOStatesCollection), bson.M{"_id": state})
	if err != nil {
		return nil, err
	}
	if shared.BoolValue(doc["used"]) {
		return nil, fmt.Errorf("state already consumed")
	}
	if isExpired(doc["expiresAt"]) {
		_ = shared.DeleteByID(ctx, shared.Collection(shared.AuthSSOStatesCollection), state)
		return nil, fmt.Errorf("state expired")
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.AuthSSOStatesCollection), state, bson.M{"used": true, "usedAt": shared.NowISO()}); err != nil {
		return nil, err
	}
	return doc, nil
}

func isExpired(raw interface{}) bool {
	value := strings.TrimSpace(shared.StringValue(raw))
	if value == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return true
	}
	return time.Now().UTC().After(parsed)
}

func sanitizeUserDocument(user bson.M) bson.M {
	out := bson.M{}
	for key, value := range user {
		if key == "password" {
			continue
		}
		out[key] = value
	}
	return out
}

func recordID(user bson.M) string {
	id := strings.TrimSpace(shared.StringValue(user["_id"]))
	if id != "" {
		return id
	}
	return strings.TrimSpace(shared.StringValue(user["id"]))
}

func nameFromClaims(firstName, lastName string) string {
	fullName := strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
	return strings.TrimSpace(fullName)
}

func pickUserInfoEndpoint(configured, discovered string) string {
	return firstNonEmpty(strings.TrimSpace(configured), strings.TrimSpace(discovered))
}

func pickIssuer(configured, discovered string) string {
	return firstNonEmpty(strings.TrimSpace(configured), strings.TrimSpace(discovered))
}

func providerNameFromIssuer(issuer string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return "OIDC"
	}
	parsed, err := url.Parse(issuer)
	if err != nil {
		return "OIDC"
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "OIDC"
	}
	return host
}

func generateSecureToken(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func addQueryParam(rawURL, key, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func redirectWithSSOError(c *gin.Context, target, code string) {
	if strings.TrimSpace(target) == "" {
		target = resolveDefaultAuthRedirect(c)
	}
	c.Redirect(http.StatusFound, addQueryParam(target, "ssoError", code))
}
