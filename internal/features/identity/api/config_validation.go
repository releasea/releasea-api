package identity

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type idpConfig struct {
	SAML         samlConfig         `json:"saml" bson:"saml"`
	OIDC         oidcConfig         `json:"oidc" bson:"oidc"`
	Provisioning provisioningConfig `json:"provisioning" bson:"provisioning"`
	Session      sessionConfig      `json:"session" bson:"session"`
	Security     securityConfig     `json:"security" bson:"security"`
}

type samlConfig struct {
	Enabled                 bool             `json:"enabled" bson:"enabled"`
	EntityID                string           `json:"entityId" bson:"entityId"`
	SSOURL                  string           `json:"ssoUrl" bson:"ssoUrl"`
	SLOURL                  string           `json:"sloUrl" bson:"sloUrl"`
	Certificate             string           `json:"certificate" bson:"certificate"`
	SignatureAlgorithm      string           `json:"signatureAlgorithm" bson:"signatureAlgorithm"`
	DigestAlgorithm         string           `json:"digestAlgorithm" bson:"digestAlgorithm"`
	NameIDFormat            string           `json:"nameIdFormat" bson:"nameIdFormat"`
	AssertionEncrypted      bool             `json:"assertionEncrypted" bson:"assertionEncrypted"`
	WantAuthnRequestsSigned bool             `json:"wantAuthnRequestsSigned" bson:"wantAuthnRequestsSigned"`
	AllowUnsolicited        bool             `json:"allowUnsolicitedResponse" bson:"allowUnsolicitedResponse"`
	AttributeMapping        attributeMapping `json:"attributeMapping" bson:"attributeMapping"`
}

type oidcConfig struct {
	Enabled           bool             `json:"enabled" bson:"enabled"`
	Issuer            string           `json:"issuer" bson:"issuer"`
	ClientID          string           `json:"clientId" bson:"clientId"`
	ClientSecret      string           `json:"clientSecret" bson:"clientSecret"`
	Scopes            []string         `json:"scopes" bson:"scopes"`
	ResponseType      string           `json:"responseType" bson:"responseType"`
	TokenEndpointAuth string           `json:"tokenEndpointAuth" bson:"tokenEndpointAuth"`
	UserinfoEndpoint  string           `json:"userinfoEndpoint" bson:"userinfoEndpoint"`
	JWKSURI           string           `json:"jwksUri" bson:"jwksUri"`
	AttributeMapping  attributeMapping `json:"attributeMapping" bson:"attributeMapping"`
}

type provisioningConfig struct {
	AutoProvision         bool   `json:"autoProvision" bson:"autoProvision"`
	AutoDeprovision       bool   `json:"autoDeprovision" bson:"autoDeprovision"`
	SyncInterval          int    `json:"syncInterval" bson:"syncInterval"`
	DefaultRole           string `json:"defaultRole" bson:"defaultRole"`
	CreateTeamsFromGroups bool   `json:"createTeamsFromGroups" bson:"createTeamsFromGroups"`
}

type sessionConfig struct {
	MaxAge       int  `json:"maxAge" bson:"maxAge"`
	IdleTimeout  int  `json:"idleTimeout" bson:"idleTimeout"`
	SingleLogout bool `json:"singleLogout" bson:"singleLogout"`
	ForceReauth  bool `json:"forceReauth" bson:"forceReauth"`
}

type securityConfig struct {
	RequireMFA     bool     `json:"requireMfa" bson:"requireMfa"`
	AllowedDomains []string `json:"allowedDomains" bson:"allowedDomains"`
	BlockedDomains []string `json:"blockedDomains" bson:"blockedDomains"`
	IPRestrictions []string `json:"ipRestrictions" bson:"ipRestrictions"`
}

type attributeMapping struct {
	Email     string `json:"email" bson:"email"`
	FirstName string `json:"firstName" bson:"firstName"`
	LastName  string `json:"lastName" bson:"lastName"`
	Groups    string `json:"groups" bson:"groups"`
}

func loadIDPConfig(ctx context.Context) (string, idpConfig, error) {
	configDoc, err := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{})
	if err != nil {
		return "", idpConfig{}, err
	}
	id := shared.StringValue(configDoc["_id"])
	if id == "" {
		return "", idpConfig{}, fmt.Errorf("identity provider config not found")
	}
	config, err := decodeIDPConfig(configDoc)
	if err != nil {
		return "", idpConfig{}, err
	}
	config.normalize()
	return id, config, nil
}

func decodeIDPConfig(doc bson.M) (idpConfig, error) {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return idpConfig{}, err
	}
	var config idpConfig
	if err := bson.Unmarshal(raw, &config); err != nil {
		return idpConfig{}, err
	}
	return config, nil
}

func (cfg idpConfig) document() (bson.M, error) {
	raw, err := bson.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var document bson.M
	if err := bson.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func (cfg *idpConfig) normalize() {
	cfg.SAML.normalize()
	cfg.OIDC.normalize()
	cfg.Provisioning.normalize()
	cfg.Session.normalize()
	cfg.Security.normalize()
}

func (cfg idpConfig) validate() error {
	if err := cfg.SAML.validate(); err != nil {
		return err
	}
	if err := cfg.OIDC.validate(); err != nil {
		return err
	}
	if err := cfg.Provisioning.validate(); err != nil {
		return err
	}
	if err := cfg.Session.validate(); err != nil {
		return err
	}
	if err := cfg.Security.validate(); err != nil {
		return err
	}
	return nil
}

func (cfg *samlConfig) normalize() {
	cfg.EntityID = strings.TrimSpace(cfg.EntityID)
	cfg.SSOURL = strings.TrimSpace(cfg.SSOURL)
	cfg.SLOURL = strings.TrimSpace(cfg.SLOURL)
	cfg.Certificate = strings.TrimSpace(cfg.Certificate)
	cfg.SignatureAlgorithm = normalizeChoice(cfg.SignatureAlgorithm, []string{"sha256", "sha512", "sha1"}, "sha256")
	cfg.DigestAlgorithm = normalizeChoice(cfg.DigestAlgorithm, []string{"sha256", "sha512", "sha1"}, "sha256")
	cfg.NameIDFormat = normalizeChoice(cfg.NameIDFormat, []string{"emailAddress", "persistent", "transient", "unspecified"}, "emailAddress")
	cfg.AttributeMapping.normalize()
}

func (cfg samlConfig) validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.EntityID == "" {
		return fmt.Errorf("SAML entityId is required when SAML is enabled")
	}
	if err := validateHTTPURL(cfg.SSOURL, "SAML ssoUrl"); err != nil {
		return err
	}
	if cfg.SLOURL != "" {
		if err := validateHTTPURL(cfg.SLOURL, "SAML sloUrl"); err != nil {
			return err
		}
	}
	if err := validateCertificate(cfg.Certificate); err != nil {
		return fmt.Errorf("SAML certificate invalid: %w", err)
	}
	if err := cfg.AttributeMapping.validate("SAML"); err != nil {
		return err
	}
	return nil
}

func (cfg *oidcConfig) normalize() {
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.Scopes = normalizeScopeList(cfg.Scopes)
	cfg.ResponseType = normalizeChoice(cfg.ResponseType, []string{"code", "id_token", "code id_token"}, "code")
	cfg.TokenEndpointAuth = normalizeChoice(cfg.TokenEndpointAuth, []string{"client_secret_basic", "client_secret_post", "private_key_jwt"}, "client_secret_post")
	cfg.UserinfoEndpoint = strings.TrimSpace(cfg.UserinfoEndpoint)
	cfg.JWKSURI = strings.TrimSpace(cfg.JWKSURI)
	cfg.AttributeMapping.normalize()
}

func (cfg oidcConfig) validate() error {
	if !cfg.Enabled {
		return nil
	}
	if err := validateHTTPURL(cfg.Issuer, "OIDC issuer"); err != nil {
		return err
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("OIDC clientId is required when OIDC is enabled")
	}
	if !containsString(cfg.Scopes, "openid") {
		return fmt.Errorf("OIDC scopes must include openid")
	}
	if cfg.TokenEndpointAuth != "private_key_jwt" && cfg.ClientSecret == "" {
		return fmt.Errorf("OIDC clientSecret is required for selected token endpoint auth mode")
	}
	if cfg.UserinfoEndpoint != "" {
		if err := validateHTTPURL(cfg.UserinfoEndpoint, "OIDC userinfoEndpoint"); err != nil {
			return err
		}
	}
	if cfg.JWKSURI != "" {
		if err := validateHTTPURL(cfg.JWKSURI, "OIDC jwksUri"); err != nil {
			return err
		}
	}
	if err := cfg.AttributeMapping.validate("OIDC"); err != nil {
		return err
	}
	return nil
}

func (cfg *provisioningConfig) normalize() {
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 60
	}
	cfg.DefaultRole = normalizeChoice(cfg.DefaultRole, []string{"admin", "developer", "viewer"}, "developer")
}

func (cfg provisioningConfig) validate() error {
	if cfg.SyncInterval < 5 || cfg.SyncInterval > 1440 {
		return fmt.Errorf("provisioning syncInterval must be between 5 and 1440 minutes")
	}
	if cfg.DefaultRole != "admin" && cfg.DefaultRole != "developer" && cfg.DefaultRole != "viewer" {
		return fmt.Errorf("provisioning defaultRole must be admin, developer, or viewer")
	}
	return nil
}

func (cfg *sessionConfig) normalize() {
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 86400
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 3600
	}
}

func (cfg sessionConfig) validate() error {
	if cfg.MaxAge < 300 || cfg.MaxAge > 2592000 {
		return fmt.Errorf("session maxAge must be between 300 and 2592000 seconds")
	}
	if cfg.IdleTimeout < 60 || cfg.IdleTimeout > cfg.MaxAge {
		return fmt.Errorf("session idleTimeout must be between 60 seconds and maxAge")
	}
	return nil
}

func (cfg *securityConfig) normalize() {
	cfg.AllowedDomains = normalizeDomainList(cfg.AllowedDomains)
	cfg.BlockedDomains = normalizeDomainList(cfg.BlockedDomains)
	cfg.IPRestrictions = normalizeIPList(cfg.IPRestrictions)
}

func (cfg securityConfig) validate() error {
	for _, domain := range cfg.AllowedDomains {
		if !isValidDomainPattern(domain) {
			return fmt.Errorf("invalid allowed domain: %s", domain)
		}
	}
	for _, domain := range cfg.BlockedDomains {
		if !isValidDomainPattern(domain) {
			return fmt.Errorf("invalid blocked domain: %s", domain)
		}
	}
	for _, value := range cfg.IPRestrictions {
		if net.ParseIP(value) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			return fmt.Errorf("invalid ip restriction: %s", value)
		}
	}
	return nil
}

func (mapping *attributeMapping) normalize() {
	mapping.Email = strings.TrimSpace(mapping.Email)
	mapping.FirstName = strings.TrimSpace(mapping.FirstName)
	mapping.LastName = strings.TrimSpace(mapping.LastName)
	mapping.Groups = strings.TrimSpace(mapping.Groups)
}

func (mapping attributeMapping) validate(protocol string) error {
	if mapping.Email == "" {
		return fmt.Errorf("%s attributeMapping.email is required", protocol)
	}
	return nil
}

func normalizeChoice(value string, allowed []string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	for _, candidate := range allowed {
		if strings.EqualFold(candidate, trimmed) {
			return candidate
		}
	}
	return fallback
}

func validateHTTPURL(raw, field string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid URL", field)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%s must use http or https", field)
	}
	return nil
}

func validateCertificate(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("certificate is required")
	}
	remaining := []byte(strings.ReplaceAll(trimmed, "\r\n", "\n"))
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return fmt.Errorf("certificate parse failed: %w", err)
			}
			return nil
		}
		remaining = rest
	}
	return fmt.Errorf("no PEM certificate block found")
}

func normalizeScopeList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, piece := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			scope := strings.ToLower(strings.TrimSpace(piece))
			if scope == "" {
				continue
			}
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			normalized = append(normalized, scope)
		}
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeDomainList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		candidate := strings.ToLower(strings.TrimSpace(value))
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, candidate)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeIPList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		candidate := strings.TrimSpace(value)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, candidate)
	}
	sort.Strings(normalized)
	return normalized
}

func isValidDomainPattern(value string) bool {
	domain := strings.ToLower(strings.TrimSpace(value))
	if domain == "" {
		return false
	}
	domain = strings.TrimPrefix(domain, "*.")
	if strings.Contains(domain, "/") || strings.Contains(domain, ":") {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) == 0 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
		for idx, char := range label {
			isAlphaNum := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
			if !isAlphaNum && char != '-' {
				return false
			}
			if char == '-' && (idx == 0 || idx == len(label)-1) {
				return false
			}
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
