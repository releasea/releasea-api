package shared

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	WorkerBootstrapProfileID = "worker-bootstrap-profile"
)

// WorkerBootstrapProfileDocument builds the canonical worker bootstrap profile
// using environment-driven runtime defaults from the current API process.
func WorkerBootstrapProfileDocument(updatedAt string) bson.M {
	platformNamespace := strings.TrimSpace(EnvOrDefault("RELEASEA_SYSTEM_NAMESPACE", "releasea-system"))
	if platformNamespace == "" {
		platformNamespace = "releasea-system"
	}

	apiBaseURL := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_API_BASE_URL",
		fmt.Sprintf("http://releasea-api.%s.svc.cluster.local:8070/api/v1", platformNamespace),
	))
	if apiBaseURL == "" {
		apiBaseURL = fmt.Sprintf("http://releasea-api.%s.svc.cluster.local:8070/api/v1", platformNamespace)
	}

	rabbitURL := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_RABBITMQ_URL",
		EnvOrDefault("RABBITMQ_URL", ""),
	))

	internalDomain := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_INTERNAL_DOMAIN",
		EnvOrDefault("RELEASEA_INTERNAL_DOMAIN", "releasea.internal"),
	))
	externalDomain := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_EXTERNAL_DOMAIN",
		EnvOrDefault("RELEASEA_EXTERNAL_DOMAIN", "releasea.external"),
	))
	internalGateway := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_INTERNAL_GATEWAY",
		EnvOrDefault("RELEASEA_INTERNAL_GATEWAY", "istio-system/releasea-internal-gateway"),
	))
	externalGateway := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_EXTERNAL_GATEWAY",
		EnvOrDefault("RELEASEA_EXTERNAL_GATEWAY", "istio-system/releasea-external-gateway"),
	))

	namespacePrefix := strings.TrimSpace(EnvOrDefault("RELEASEA_WORKER_BOOTSTRAP_NAMESPACE_PREFIX", "releasea-apps"))
	if namespacePrefix == "" {
		namespacePrefix = "releasea-apps"
	}

	minioEndpoint := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_MINIO_ENDPOINT",
		EnvOrDefault("RELEASEA_MINIO_ENDPOINT", fmt.Sprintf("releasea-minio.%s.svc.cluster.local:9000", platformNamespace)),
	))
	minioBucket := strings.TrimSpace(EnvOrDefault("RELEASEA_WORKER_BOOTSTRAP_MINIO_BUCKET", EnvOrDefault("RELEASEA_MINIO_BUCKET", "releasea-static")))
	minioSecure := EnvBool("RELEASEA_WORKER_BOOTSTRAP_MINIO_SECURE", EnvBool("RELEASEA_MINIO_SECURE", false))

	staticNginxService := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_STATIC_NGINX_SERVICE",
		EnvOrDefault("RELEASEA_STATIC_NGINX_SERVICE", "releasea-static-nginx"),
	))
	if staticNginxService == "" {
		staticNginxService = "releasea-static-nginx"
	}

	staticNginxNamespace := strings.TrimSpace(EnvOrDefault(
		"RELEASEA_WORKER_BOOTSTRAP_STATIC_NGINX_NAMESPACE",
		EnvOrDefault("RELEASEA_STATIC_NGINX_NAMESPACE", platformNamespace),
	))
	if staticNginxNamespace == "" {
		staticNginxNamespace = platformNamespace
	}

	mode := strings.TrimSpace(EnvOrDefault("RELEASEA_WORKER_BOOTSTRAP_MODE", "same-cluster"))
	if mode == "" {
		mode = "same-cluster"
	}

	version := strings.TrimSpace(EnvOrDefault("RELEASEA_WORKER_BOOTSTRAP_VERSION", "1"))
	if version == "" {
		version = "1"
	}

	configMapName := strings.TrimSpace(EnvOrDefault("RELEASEA_WORKER_BOOTSTRAP_CONFIGMAP", "releasea-worker-bootstrap"))
	if configMapName == "" {
		configMapName = "releasea-worker-bootstrap"
	}

	secretName := strings.TrimSpace(EnvOrDefault("RELEASEA_WORKER_BOOTSTRAP_SECRET", "releasea-worker-bootstrap"))
	if secretName == "" {
		secretName = "releasea-worker-bootstrap"
	}

	return bson.M{
		"_id":                  WorkerBootstrapProfileID,
		"id":                   WorkerBootstrapProfileID,
		"mode":                 mode,
		"version":              version,
		"platformNamespace":    platformNamespace,
		"apiBaseUrl":           apiBaseURL,
		"rabbitmqUrl":          rabbitURL,
		"internalDomain":       internalDomain,
		"externalDomain":       externalDomain,
		"internalGateway":      internalGateway,
		"externalGateway":      externalGateway,
		"namespacePrefix":      namespacePrefix,
		"minioEndpoint":        minioEndpoint,
		"minioBucket":          minioBucket,
		"minioSecure":          minioSecure,
		"staticNginxService":   staticNginxService,
		"staticNginxNamespace": staticNginxNamespace,
		"source": bson.M{
			"configMap": configMapName,
			"secret":    secretName,
		},
		"updatedAt": updatedAt,
	}
}
