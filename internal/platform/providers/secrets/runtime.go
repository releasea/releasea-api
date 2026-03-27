package secretsproviders

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type Provider struct {
	ID     string
	Type   string
	Config map[string]interface{}
}

type Runtime interface {
	ID() string
	Resolve(ctx context.Context, provider *Provider, ref string) (string, error)
	HealthCheck(ctx context.Context, provider *Provider) error
}

func Normalize(providerType string) string {
	return strings.ToLower(strings.TrimSpace(providerType))
}

func ParseProvider(raw bson.M) *Provider {
	if len(raw) == 0 {
		return nil
	}
	provider := &Provider{
		ID:     shared.StringValue(raw["id"]),
		Type:   Normalize(shared.StringValue(raw["type"])),
		Config: map[string]interface{}{},
	}
	if provider.ID == "" {
		provider.ID = shared.StringValue(raw["_id"])
	}
	switch config := raw["config"].(type) {
	case bson.M:
		provider.Config = map[string]interface{}(config)
	case map[string]interface{}:
		provider.Config = config
	}
	return provider
}

func IsSecretRef(value string) bool {
	return strings.HasPrefix(value, "vault://") ||
		strings.HasPrefix(value, "aws://") ||
		strings.HasPrefix(value, "gcp://") ||
		strings.HasPrefix(value, "secret://")
}

func ResolveConfiguredProviderDocument(settings bson.M, requestedProviderID string) bson.M {
	secrets := nestedMap(settings["secrets"])
	defaultID := shared.StringValue(secrets["defaultProviderId"])
	providerID := strings.TrimSpace(requestedProviderID)
	if providerID == "" {
		providerID = defaultID
	}
	if providerID == "" {
		return bson.M{}
	}
	for _, item := range interfaceSlice(secrets["providers"]) {
		provider := nestedMap(item)
		if shared.StringValue(provider["id"]) == providerID {
			return provider
		}
	}
	return bson.M{}
}

func ResolveReference(ctx context.Context, provider *Provider, environment, value string) (string, error) {
	value = strings.ReplaceAll(value, "{{env}}", environment)
	if strings.HasPrefix(value, "secret://") {
		if provider == nil {
			return "", errors.New("secret provider not configured")
		}
		value = strings.Replace(value, "secret://", provider.Type+"://", 1)
	}
	switch {
	case strings.HasPrefix(value, "vault://"):
		if provider == nil || provider.Type != "vault" {
			return "", errors.New("vault provider not configured")
		}
		runtime, _ := ResolveRuntime(provider.Type)
		return runtime.Resolve(ctx, provider, strings.TrimPrefix(value, "vault://"))
	case strings.HasPrefix(value, "aws://"):
		if provider == nil || provider.Type != "aws" {
			return "", errors.New("aws provider not configured")
		}
		runtime, _ := ResolveRuntime(provider.Type)
		return runtime.Resolve(ctx, provider, strings.TrimPrefix(value, "aws://"))
	case strings.HasPrefix(value, "gcp://"):
		if provider == nil || provider.Type != "gcp" {
			return "", errors.New("gcp provider not configured")
		}
		runtime, _ := ResolveRuntime(provider.Type)
		return runtime.Resolve(ctx, provider, strings.TrimPrefix(value, "gcp://"))
	default:
		return "", errors.New("unsupported secret reference")
	}
}

func ResolveRuntime(providerType string) (Runtime, bool) {
	switch Normalize(providerType) {
	case "vault":
		return vaultRuntime{}, true
	case "aws":
		return awsRuntime{}, true
	case "gcp":
		return gcpRuntime{}, true
	default:
		return nil, false
	}
}

type vaultRuntime struct{}

func (vaultRuntime) ID() string { return "vault" }

func (vaultRuntime) HealthCheck(ctx context.Context, provider *Provider) error {
	address := strings.TrimSpace(mapStringValue(provider.Config, "address"))
	token := strings.TrimSpace(mapStringValue(provider.Config, "token"))
	if address == "" || token == "" {
		return errors.New("vault address/token missing")
	}
	endpoint := strings.TrimRight(address, "/") + "/v1/auth/token/lookup-self"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("vault healthcheck failed: %s", resp.Status)
	}
	return nil
}

func (vaultRuntime) Resolve(ctx context.Context, provider *Provider, ref string) (string, error) {
	address := strings.TrimSpace(mapStringValue(provider.Config, "address"))
	token := strings.TrimSpace(mapStringValue(provider.Config, "token"))
	if address == "" || token == "" {
		return "", errors.New("vault address/token missing")
	}
	path, key := splitSecretRef(ref)
	if path == "" {
		return "", errors.New("vault secret path missing")
	}
	endpoint := strings.TrimRight(address, "/") + "/v1/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("vault request failed: %s", resp.Status)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	data := mapValue(payload["data"])
	if nested := mapValue(data["data"]); len(nested) > 0 {
		data = nested
	}
	return extractSecretValue(data, key)
}

type awsRuntime struct{}

func (awsRuntime) ID() string { return "aws" }

func (awsRuntime) HealthCheck(ctx context.Context, provider *Provider) error {
	accessKey := mapStringValue(provider.Config, "accessKeyId")
	secretKey := mapStringValue(provider.Config, "secretAccessKey")
	region := mapStringValue(provider.Config, "region")
	if region == "" {
		region = "us-east-1"
	}
	if accessKey == "" || secretKey == "" {
		return errors.New("aws credentials missing")
	}

	payload := []byte("Action=GetCallerIdentity&Version=2011-06-15")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, awsSTSURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Amz-Target", "GetCallerIdentity")

	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := time.Now().UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)

	signedHeaders, authHeader, err := signAwsRequest(req, payload, accessKey, secretKey, region, "sts", amzDate, dateStamp)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-Amz-Content-Sha256", hashSHA256(payload))
	req.Header.Set("SignedHeaders", signedHeaders)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("aws healthcheck failed: %s", resp.Status)
	}
	return nil
}

func (awsRuntime) Resolve(ctx context.Context, provider *Provider, ref string) (string, error) {
	accessKey := mapStringValue(provider.Config, "accessKeyId")
	secretKey := mapStringValue(provider.Config, "secretAccessKey")
	region := mapStringValue(provider.Config, "region")
	if region == "" {
		region = "us-east-1"
	}
	if accessKey == "" || secretKey == "" {
		return "", errors.New("aws credentials missing")
	}
	secretName, jsonKey := splitSecretRef(ref)
	if secretName == "" {
		return "", errors.New("aws secret name missing")
	}
	endpoint := fmt.Sprintf("https://secretsmanager.%s.amazonaws.com/", region)
	body := map[string]string{"SecretId": secretName}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager.GetSecretValue")

	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := time.Now().UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)

	signedHeaders, authHeader, err := signAwsRequest(req, payload, accessKey, secretKey, region, "secretsmanager", amzDate, dateStamp)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-Amz-Content-Sha256", hashSHA256(payload))
	req.Header.Set("SignedHeaders", signedHeaders)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("aws secret request failed: %s", resp.Status)
	}
	var response struct {
		SecretString string `json:"SecretString"`
		SecretBinary string `json:"SecretBinary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	secretValue := response.SecretString
	if secretValue == "" && response.SecretBinary != "" {
		decoded, err := base64.StdEncoding.DecodeString(response.SecretBinary)
		if err != nil {
			return "", err
		}
		secretValue = string(decoded)
	}
	if jsonKey == "" {
		return secretValue, nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(secretValue), &data); err != nil {
		return "", err
	}
	return extractSecretValue(data, jsonKey)
}

type gcpRuntime struct{}

func (gcpRuntime) ID() string { return "gcp" }

func (gcpRuntime) HealthCheck(ctx context.Context, provider *Provider) error {
	serviceAccount := mapStringValue(provider.Config, "serviceAccountJson")
	projectID := mapStringValue(provider.Config, "projectId")
	if projectID == "" {
		return errors.New("gcp project id missing")
	}
	token, err := fetchGcpAccessToken(ctx, serviceAccount)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/v1/projects/%s/secrets?pageSize=1", strings.TrimRight(gcpSecretManagerBaseURL, "/"), projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gcp healthcheck failed: %s", resp.Status)
	}
	return nil
}

func (gcpRuntime) Resolve(ctx context.Context, provider *Provider, ref string) (string, error) {
	serviceAccount := mapStringValue(provider.Config, "serviceAccountJson")
	projectID := mapStringValue(provider.Config, "projectId")
	secretRef, version := splitSecretRef(ref)
	if secretRef == "" {
		return "", errors.New("gcp secret name missing")
	}
	if strings.Contains(secretRef, "/") {
		parts := strings.SplitN(secretRef, "/", 2)
		projectID = parts[0]
		secretRef = parts[1]
	}
	if projectID == "" {
		return "", errors.New("gcp project id missing")
	}
	if version == "" {
		version = "latest"
	}
	token, err := fetchGcpAccessToken(ctx, serviceAccount)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/secrets/%s/versions/%s:access", strings.TrimRight(gcpSecretManagerBaseURL, "/"), projectID, secretRef, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gcp secret request failed: %s", resp.Status)
	}
	var response struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	if response.Payload.Data == "" {
		return "", errors.New("gcp secret payload empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(response.Payload.Data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

var (
	awsSTSURL               = "https://sts.amazonaws.com/"
	gcpSecretManagerBaseURL = "https://secretmanager.googleapis.com"
)

func fetchGcpAccessToken(ctx context.Context, serviceAccountJSON string) (string, error) {
	if serviceAccountJSON == "" {
		return "", errors.New("gcp service account json missing")
	}
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(serviceAccountJSON), &sa); err != nil {
		return "", err
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return "", errors.New("gcp service account invalid")
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	signedJWT, err := buildGoogleServiceAccountJWT(sa.ClientEmail, sa.PrivateKey, tokenURI)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", signedJWT)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gcp token request failed: %s", resp.Status)
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", err
	}
	if tokenResponse.AccessToken == "" {
		return "", errors.New("gcp access token missing")
	}
	return tokenResponse.AccessToken, nil
}

func buildGoogleServiceAccountJWT(clientEmail, privateKeyPEM, audience string) (string, error) {
	keyBlock, _ := pem.Decode([]byte(privateKeyPEM))
	if keyBlock == nil {
		return "", errors.New("invalid gcp private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		pkcs1Key, pkcs1Err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
		if pkcs1Err != nil {
			return "", err
		}
		key = pkcs1Key
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("gcp private key must be RSA")
	}
	now := time.Now().Unix()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]interface{}{
		"iss":   clientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   audience,
		"exp":   now + 3600,
		"iat":   now,
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func signAwsRequest(req *http.Request, payload []byte, accessKey, secretKey, region, service, amzDate, dateStamp string) (string, string, error) {
	canonicalHeaders := fmt.Sprintf(
		"content-type:%s\nhost:%s\nx-amz-date:%s\nx-amz-target:%s\n",
		req.Header.Get("Content-Type"),
		req.URL.Host,
		req.Header.Get("X-Amz-Date"),
		req.Header.Get("X-Amz-Target"),
	)
	signedHeaders := "content-type;host;x-amz-date;x-amz-target"
	payloadHash := hashSHA256(payload)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := awsSignKey(secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(awsHmac(signingKey, stringToSign))
	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey,
		scope,
		signedHeaders,
		signature,
	)
	return signedHeaders, authHeader, nil
}

func awsSignKey(secretKey, dateStamp, region, service string) []byte {
	kDate := awsHmac([]byte("AWS4"+secretKey), dateStamp)
	kRegion := awsHmac(kDate, region)
	kService := awsHmac(kRegion, service)
	return awsHmac(kService, "aws4_request")
}

func awsHmac(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func hashSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func splitSecretRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func mapValue(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	case bson.M:
		return map[string]interface{}(typed)
	default:
		return map[string]interface{}{}
	}
}

func extractSecretValue(data map[string]interface{}, key string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("secret payload empty")
	}
	if key == "" {
		for _, value := range data {
			if str, ok := value.(string); ok {
				return str, nil
			}
		}
		return "", errors.New("secret key missing")
	}
	raw, ok := data[key]
	if !ok {
		return "", fmt.Errorf("secret key %s not found", key)
	}
	if str, ok := raw.(string); ok {
		return str, nil
	}
	return "", errors.New("secret value is not a string")
}

func mapStringValue(config map[string]interface{}, key string) string {
	value, _ := config[key]
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func nestedMap(value interface{}) bson.M {
	switch typed := value.(type) {
	case bson.M:
		return typed
	case map[string]interface{}:
		return bson.M(typed)
	default:
		return bson.M{}
	}
}

func interfaceSlice(value interface{}) []interface{} {
	if items, ok := value.([]interface{}); ok {
		return items
	}
	return nil
}
