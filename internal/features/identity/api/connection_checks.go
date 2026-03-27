package identity

import (
	"context"
	"fmt"
	"io"
	"net/http"
	identityproviders "releaseaapi/internal/platform/providers/identity"
	"strings"
	"time"

	httpclient "releaseaapi/internal/platform/http/client"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
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
	runtime, err := identityproviders.ResolveRuntime(protocol)
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Unsupported protocol. Use saml or oidc")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	configDoc, err := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Identity provider config not found")
		return
	}
	config := nestedIdentityConfig(configDoc, protocol)
	if err := runtime.ValidateConfiguration(config); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := runtime.TestConnection(ctx, config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("%s connection test failed: %v", strings.ToUpper(protocol), err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": strings.ToUpper(protocol) + " connection verified successfully",
	})
}

func nestedIdentityConfig(document bson.M, key string) bson.M {
	value := document[key]
	switch typed := value.(type) {
	case bson.M:
		return typed
	case map[string]interface{}:
		return bson.M(typed)
	default:
		return bson.M{}
	}
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
	base := strings.TrimSpace(issuer)
	base = strings.TrimSuffix(base, "/")
	return base + "/.well-known/openid-configuration"
}
