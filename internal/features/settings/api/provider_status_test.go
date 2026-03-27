package settings

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

const testSAMLPEM = `-----BEGIN CERTIFICATE-----
MIIEaTCCAtGgAwIBAgIRAPi+ogrk2xiDoiNH6MYq8MowDQYJKoZIhvcNAQELBQAw
aTEeMBwGA1UEChMVbWtjZXJ0IGRldmVsb3BtZW50IENBMR8wHQYDVQQLDBZtYWlh
QG1haWEgKERpZWdvIE1haWEpMSYwJAYDVQQDDB1ta2NlcnQgbWFpYUBtYWlhIChE
aWVnbyBNYWlhKTAeFw0yNjAyMjIyMTM0MzZaFw0yODA1MjIyMTM0MzZaMFAxJzAl
BgNVBAoTHm1rY2VydCBkZXZlbG9wbWVudCBjZXJ0aWZpY2F0ZTElMCMGA1UECwwc
bWFpYUBtYWlhLmxvY2FsIChEaWVnbyBNYWlhKTCCASIwDQYJKoZIhvcNAQEBBQAD
ggEPADCCAQoCggEBAK8Q+ONbXC9KjgYiSgFjBPs/Em0fNLPTnXo3fFpHHSQ06q8y
mnW3mAYRwL3deTs/e1Njghnn+YPdWH31cifG/IjRQyTriDaVcLZtbv+ZnMcqrRAl
8lTooaBXlCA7JBpFwgkkCmqDS2geSIhK27iUijJO/cfRnYHHlxWS/cQsLArmSagP
b2p7l33X4OYC29EIJ0HzbpoQW1w35PN/Mw+Rnt9Fe+U94oMmHJXAa6sMqs0txSOq
54ExrI9yFOSgOtZ6LxROn+3gH8cCOuQJheUxY5MSPQ0oG7hAN5CM67/vK/zma9c3
3x6WfDjOFYkrBbFOJXIizirFU+yxFZGZBwCccQECAwEAAaOBpDCBoTAOBgNVHQ8B
Af8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwEwHwYDVR0jBBgwFoAUzdnODLsH
MFKKkqQN1u2I1ypPLakwWQYDVR0RBFIwUIITKi5yZWxlYXNlYS5pbnRlcm5hbIIT
Ki5yZWxlYXNlYS5leHRlcm5hbIIRcmVsZWFzZWEuaW50ZXJuYWyCEXJlbGVhc2Vh
LmV4dGVybmFsMA0GCSqGSIb3DQEBCwUAA4IBgQCPjw+spDl1BMb02OCLv8VcdSUj
d8n8hwhA7KPuajkH/EyA+Q+joqQEWB+cO6SL9Li+F7xU6oz0BHKolT7eYTF98b/C
0tiP/Ofm+3ljQIg7hAmelb2piSHBDLKdE82dXbGDxSk3ElLmY5p+hTO9QFnJp41W
BEXPXuqwW6LteHdLgLuU9GTjYQZpTX5NECPANUp5zmBNS0k49SfjcM6vApnzLXij
aYoenrAfz5LEkpoQ2IzkzpBE/qUYQpWKaFapN2LFQsDBa2scl2Y+7mj8dhZsme8u
tTxlT2wOawSg4v7xkrjXg25aNSWRoKWfXUZBsgOUsjsNj49DtKTUXUmchO/Bve5U
J1APcBGk4UQ1fiVmqJWxlsueGTF9f3Kdpbd/9+MDlMrPJ8JK/3f74MhhdkMzW9wZ
vHlqBQVbBPlKZURdnyQUW5lcQ021QXHASjfQQUcXBodGOqVb351q0Qk1NfLjJExi
Whs2F87+/MutNUK3lRD9NBFUaM6AxUAxiU7Gf1s=
-----END CERTIFICATE-----`

func TestBuildProviderStatusCatalog(t *testing.T) {
	catalog := buildProviderCatalog()
	settings := bson.M{
		"secrets": bson.M{
			"defaultProviderId": "sp-1",
			"providers": []interface{}{
				bson.M{"id": "sp-1", "type": "vault", "status": "connected"},
				bson.M{"id": "sp-2", "type": "aws", "status": "disconnected"},
			},
		},
		"notifications": bson.M{
			"deploySuccess": true,
			"deployFailed":  false,
			"serviceDown":   true,
		},
	}
	idp := bson.M{
		"saml": bson.M{
			"enabled":     true,
			"entityId":    "releasea",
			"ssoUrl":      "https://idp.example/sso",
			"certificate": testSAMLPEM,
		},
		"oidc": bson.M{
			"enabled":  true,
			"issuer":   "https://issuer.example",
			"clientId": "",
		},
	}
	scmCreds := []bson.M{
		{"provider": "github"},
		{"provider": "github"},
	}
	registryCreds := []bson.M{
		{"provider": "ghcr"},
	}

	status := buildProviderStatusCatalog(catalog, settings, idp, scmCreds, registryCreds)

	if got := status.SCM.Providers[0].State; got != providerStateConfigured {
		t.Fatalf("github scm state = %q, want %q", got, providerStateConfigured)
	}
	if got := status.SCM.Providers[0].ResourceCount; got != 2 {
		t.Fatalf("github scm resourceCount = %d, want %d", got, 2)
	}
	if got := status.Registry.Providers[1].State; got != providerStateConfigured {
		t.Fatalf("ghcr registry state = %q, want %q", got, providerStateConfigured)
	}
	if got := status.Secrets.Providers[0].Default; !got {
		t.Fatalf("vault should be marked as default secret provider")
	}
	if got := status.Secrets.Providers[1].State; got != providerStatePartial {
		t.Fatalf("aws secrets state = %q, want %q", got, providerStatePartial)
	}
	if got := status.Identity.Providers[0].State; got != providerStateConfigured {
		t.Fatalf("saml identity state = %q, want %q", got, providerStateConfigured)
	}
	if got := status.Identity.Providers[1].State; got != providerStatePartial {
		t.Fatalf("oidc identity state = %q, want %q", got, providerStatePartial)
	}
	if got := status.Notifications.Providers[0].State; got != providerStateConfigured {
		t.Fatalf("notifications state = %q, want %q", got, providerStateConfigured)
	}
}
