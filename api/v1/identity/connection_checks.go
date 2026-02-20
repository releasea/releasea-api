package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
)

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

func TestIdpConnection(c *gin.Context) {
	protocol := strings.ToLower(strings.TrimSpace(c.Param("protocol")))
	if protocol != "saml" && protocol != "oidc" {
		shared.RespondError(c, http.StatusBadRequest, "Unsupported protocol. Use saml or oidc")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, config, err := loadIDPConfig(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Identity provider config not found")
		return
	}
	if err := config.validate(); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	switch protocol {
	case "saml":
		if err := testSAMLConnection(ctx, config.SAML); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("SAML connection test failed: %v", err),
			})
			return
		}
	case "oidc":
		if err := testOIDCConnection(ctx, config.OIDC); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("OIDC connection test failed: %v", err),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": strings.ToUpper(protocol) + " connection verified successfully",
	})
}

func testSAMLConnection(ctx context.Context, cfg samlConfig) error {
	if !cfg.Enabled {
		return fmt.Errorf("SAML is disabled")
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := probeEndpoint(ctx, cfg.SSOURL); err != nil {
		return fmt.Errorf("unable to reach ssoUrl: %w", err)
	}
	if cfg.SLOURL != "" {
		if err := probeEndpoint(ctx, cfg.SLOURL); err != nil {
			return fmt.Errorf("unable to reach sloUrl: %w", err)
		}
	}
	return nil
}

func testOIDCConnection(ctx context.Context, cfg oidcConfig) error {
	if !cfg.Enabled {
		return fmt.Errorf("OIDC is disabled")
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	discoveryURL := buildOIDCDiscoveryURL(cfg.Issuer)
	discoveryRaw, err := fetchHTTPBody(ctx, discoveryURL)
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

func probeEndpoint(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
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
	client := &http.Client{Timeout: 10 * time.Second}
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
	base := strings.TrimSpace(issuer)
	base = strings.TrimSuffix(base, "/")
	return base + "/.well-known/openid-configuration"
}
