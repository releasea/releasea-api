package identityproviders

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	httpclient "releaseaapi/internal/platform/http/client"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

type samlRuntime struct{}
type oidcRuntime struct{}

type samlConfig struct {
	Enabled     bool   `bson:"enabled"`
	EntityID    string `bson:"entityId"`
	SSOURL      string `bson:"ssoUrl"`
	SLOURL      string `bson:"sloUrl"`
	Certificate string `bson:"certificate"`
}

type oidcConfig struct {
	Enabled           bool     `bson:"enabled"`
	Issuer            string   `bson:"issuer"`
	ClientID          string   `bson:"clientId"`
	ClientSecret      string   `bson:"clientSecret"`
	Scopes            []string `bson:"scopes"`
	TokenEndpointAuth string   `bson:"tokenEndpointAuth"`
	UserinfoEndpoint  string   `bson:"userinfoEndpoint"`
	JWKSURI           string   `bson:"jwksUri"`
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type oidcJWKS struct {
	Keys []map[string]interface{} `json:"keys"`
}

func (samlRuntime) ID() string { return "saml" }
func (oidcRuntime) ID() string { return "oidc" }

func (samlRuntime) ValidateConfiguration(config bson.M) error {
	cfg, err := decodeSAMLConfig(config)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("SAML is disabled")
	}
	if strings.TrimSpace(cfg.EntityID) == "" {
		return fmt.Errorf("SAML entityId is required when SAML is enabled")
	}
	if err := validateHTTPURL(cfg.SSOURL, "SAML ssoUrl"); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.SLOURL) != "" {
		if err := validateHTTPURL(cfg.SLOURL, "SAML sloUrl"); err != nil {
			return err
		}
	}
	if err := validateCertificate(cfg.Certificate); err != nil {
		return fmt.Errorf("SAML certificate invalid: %w", err)
	}
	return nil
}

func (samlRuntime) TestConnection(ctx context.Context, config bson.M) error {
	cfg, err := decodeSAMLConfig(config)
	if err != nil {
		return err
	}
	if err := (samlRuntime{}).ValidateConfiguration(config); err != nil {
		return err
	}
	if err := probeEndpoint(ctx, cfg.SSOURL); err != nil {
		return fmt.Errorf("unable to reach ssoUrl: %w", err)
	}
	if strings.TrimSpace(cfg.SLOURL) != "" {
		if err := probeEndpoint(ctx, cfg.SLOURL); err != nil {
			return fmt.Errorf("unable to reach sloUrl: %w", err)
		}
	}
	return nil
}

func (oidcRuntime) ValidateConfiguration(config bson.M) error {
	cfg, err := decodeOIDCConfig(config)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("OIDC is disabled")
	}
	if err := validateHTTPURL(cfg.Issuer, "OIDC issuer"); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return fmt.Errorf("OIDC clientId is required when OIDC is enabled")
	}
	scopes := normalizeScopeList(cfg.Scopes)
	if !containsString(scopes, "openid") {
		return fmt.Errorf("OIDC scopes must include openid")
	}
	authMode := normalizeChoice(cfg.TokenEndpointAuth, []string{"client_secret_basic", "client_secret_post", "private_key_jwt"}, "client_secret_post")
	if authMode != "private_key_jwt" && strings.TrimSpace(cfg.ClientSecret) == "" {
		return fmt.Errorf("OIDC clientSecret is required for selected token endpoint auth mode")
	}
	if strings.TrimSpace(cfg.UserinfoEndpoint) != "" {
		if err := validateHTTPURL(cfg.UserinfoEndpoint, "OIDC userinfoEndpoint"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.JWKSURI) != "" {
		if err := validateHTTPURL(cfg.JWKSURI, "OIDC jwksUri"); err != nil {
			return err
		}
	}
	return nil
}

func (oidcRuntime) TestConnection(ctx context.Context, config bson.M) error {
	cfg, err := decodeOIDCConfig(config)
	if err != nil {
		return err
	}
	if err := (oidcRuntime{}).ValidateConfiguration(config); err != nil {
		return err
	}

	discoveryRaw, err := fetchHTTPBody(ctx, buildOIDCDiscoveryURL(cfg.Issuer))
	if err != nil {
		return fmt.Errorf("discovery fetch failed: %w", err)
	}

	var discovery oidcDiscovery
	if err := json.Unmarshal(discoveryRaw, &discovery); err != nil {
		return fmt.Errorf("invalid discovery response")
	}
	if strings.TrimSpace(discovery.Issuer) == "" {
		return fmt.Errorf("discovery missing issuer")
	}
	if strings.TrimSpace(discovery.AuthorizationEndpoint) == "" {
		return fmt.Errorf("discovery missing authorization_endpoint")
	}
	if strings.TrimSpace(discovery.TokenEndpoint) == "" {
		return fmt.Errorf("discovery missing token_endpoint")
	}

	jwksURI := strings.TrimSpace(discovery.JWKSURI)
	if strings.TrimSpace(cfg.JWKSURI) != "" {
		jwksURI = strings.TrimSpace(cfg.JWKSURI)
	}
	if jwksURI == "" {
		return fmt.Errorf("discovery missing jwks_uri")
	}
	jwksRaw, err := fetchHTTPBody(ctx, jwksURI)
	if err != nil {
		return fmt.Errorf("jwks fetch failed: %w", err)
	}
	var jwks oidcJWKS
	if err := json.Unmarshal(jwksRaw, &jwks); err != nil {
		return fmt.Errorf("invalid jwks response")
	}
	if len(jwks.Keys) == 0 {
		return fmt.Errorf("jwks has no keys")
	}

	if strings.TrimSpace(cfg.UserinfoEndpoint) != "" {
		if err := probeEndpoint(ctx, cfg.UserinfoEndpoint); err != nil {
			return fmt.Errorf("unable to reach configured userinfoEndpoint: %w", err)
		}
	}
	return nil
}

func decodeSAMLConfig(config bson.M) (samlConfig, error) {
	raw, err := bson.Marshal(config)
	if err != nil {
		return samlConfig{}, err
	}
	var decoded samlConfig
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		return samlConfig{}, err
	}
	return decoded, nil
}

func decodeOIDCConfig(config bson.M) (oidcConfig, error) {
	raw, err := bson.Marshal(config)
	if err != nil {
		return oidcConfig{}, err
	}
	var decoded oidcConfig
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		return oidcConfig{}, err
	}
	return decoded, nil
}

func probeEndpoint(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := httpclient.New(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

func fetchHTTPBody(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := httpclient.New(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
}

func buildOIDCDiscoveryURL(issuer string) string {
	base := strings.TrimSuffix(strings.TrimSpace(issuer), "/")
	return base + "/.well-known/openid-configuration"
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
	return normalized
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

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(expected)) {
			return true
		}
	}
	return false
}
