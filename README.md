# Releasea API

REST API server for the Releasea platform (Go + Gin + MongoDB).

## Overview

The API is the control plane for services, deploy operations, workers, governance, identity, and real-time status streaming.

## Running Locally

```bash
go mod download
go run ./cmd/main.go
```

## Quality Commands

```bash
make quality
```

Common targets:

- `make fmt`
- `make fmt-check`
- `make vet`
- `make test-race`
- `make architecture-check`
- `make coverage-check`
- `make lint`

## Environment Variables

### Core

| Variable | Description | Default |
|---|---|---|
| `PORT` | HTTP port for the API server | `8070` |
| `MONGO_URI` | MongoDB connection string | `mongodb://localhost:27017/releasea` |
| `MONGO_TLS_INSECURE` | Skip MongoDB TLS verification (dev only) | `false` |

### Authentication

| Variable | Description | Default |
|---|---|---|
| `JWT_SECRET` | Access token signing secret | `change-me` |
| `JWT_TTL_MINUTES` | Access token TTL (minutes) | `720` |
| `JWT_REFRESH_SECRET` | Refresh token signing secret (falls back to `JWT_SECRET` when empty) | _(empty)_ |
| `JWT_REFRESH_TTL_HOURS` | Refresh token TTL (hours) | `720` |
| `WORKER_JWT_SECRET` | Worker token signing secret | `change-me` |
| `WORKER_JWT_TTL_MINUTES` | Worker token TTL (minutes) | `30` |

### Setup and Templates

| Variable | Description | Default |
|---|---|---|
| `RELEASEA_RESET` | When `true`, drops the database and restores base defaults on startup (destructive) | `false` |
| `INSTALL_TEMPLATES` | Enables template installation from the templates repository | `true` |
| `TEMPLATE_REPO_OWNER` | Templates repository owner | `releasea` |
| `TEMPLATE_REPO_NAME` | Templates repository name | `templates` |
| `TEMPLATE_REPO_REF` | Branch/tag used to fetch templates | `main` |

Behavior summary:

- First run on an empty database: the API bootstraps base data automatically.
- Subsequent restarts: no reset is performed by default.
- Forced reset/restore: set `RELEASEA_RESET=true` deliberately.

### Bootstrap Identity

| Variable | Description | Default |
|---|---|---|
| `DEFAULT_TEAM_ID` | Default team ID created/ensured at startup | `team-1` |
| `DEFAULT_TEAM_NAME` | Default team display name | `Platform` |
| `DEFAULT_ADMIN_ID` | Default admin user ID | `user-1` |
| `DEFAULT_ADMIN_NAME` | Default admin display name | `Platform Admin` |
| `DEFAULT_ADMIN_EMAIL` | Default admin email | `admin@releasea.io` |
| `DEFAULT_ADMIN_PASSWORD` | Default admin password | `releasea` |
| `ALLOW_USER_SIGNUP` | Enables public sign-up endpoint | `false` |
| `KEEP_ADDITIONAL_USERS` | Keeps extra users/profiles during bootstrap identity reconciliation | `false` |

### Queue and Worker Validation

| Variable | Description | Default |
|---|---|---|
| `RABBITMQ_URL` | RabbitMQ AMQP URL | `amqp://releasea:releasea@localhost:5672/` |
| `WORKER_QUEUE` | Queue name consumed by workers | `releasea.worker` |
| `WORKER_STALE_SECONDS` | Worker stale timeout used for environment-availability checks on deploy/start/stop/restart/rule publish | `90` |

### Observability and Runtime Status

| Variable | Description | Default |
|---|---|---|
| `PROMETHEUS_URL` | Prometheus base URL | `http://localhost:9090` |
| `LOKI_URL` | Loki base URL | `http://localhost:3100` |
| `STATUS_STREAM_POLL_SECONDS` | Fallback polling interval used by status stream internals | `15` |

### Network and Routing

| Variable | Description | Default |
|---|---|---|
| `RELEASEA_SYSTEM_NAMESPACE` | Namespace where platform shared workloads run | `releasea-system` |
| `RELEASEA_STATIC_NGINX_WORKLOAD` | Static workload used for aggregate static metrics | `releasea-static-nginx` |
| `RELEASEA_STATIC_METRICS_SCOPE` | Static metrics mode (`aggregate` or service-scoped) | `aggregate` |
| `RELEASEA_INTERNAL_DOMAIN` | Internal base domain for generated hosts | `releasea.internal` |
| `RELEASEA_EXTERNAL_DOMAIN` | External base domain for generated hosts | `releasea.external` |
| `RELEASEA_INTERNAL_GATEWAY` | Istio internal gateway reference | `istio-system/releasea-internal-gateway` |
| `RELEASEA_EXTERNAL_GATEWAY` | Istio external gateway reference | `istio-system/releasea-external-gateway` |
| `RELEASEA_NAMESPACE_MAPPING` | Optional JSON map to override environment-to-namespace resolution | _(empty)_ |

### Optional Integrations

| Variable | Description | Default |
|---|---|---|
| `RELEASEA_NOTIFICATIONS_WEBHOOK_URL` | Primary webhook for operation notifications | _(empty)_ |
| `NOTIFICATIONS_WEBHOOK_URL` | Legacy webhook fallback | _(empty)_ |
| `AUTH_SSO_CALLBACK_URL` | SSO callback URL override | _(empty)_ |
| `AUTH_SSO_REDIRECT_URL` | SSO post-login redirect URL override | _(empty)_ |
| `CORS_ORIGINS` | Allowed CORS origins (comma-separated) | _(empty)_ |

### RabbitMQ TLS (Optional)

| Variable | Description | Default |
|---|---|---|
| `RABBITMQ_TLS_ENABLE` | Enables TLS for RabbitMQ connection | `false` |
| `RABBITMQ_TLS_SERVER_NAME` | TLS server name override | _(empty)_ |
| `RABBITMQ_TLS_CA_PATH` | CA bundle path | _(empty)_ |
| `RABBITMQ_TLS_CERT_PATH` | Client cert path | _(empty)_ |
| `RABBITMQ_TLS_KEY_PATH` | Client key path | _(empty)_ |
| `RABBITMQ_TLS_INSECURE` | Skip TLS verification (dev only) | `false` |

### Debug Flags (Optional)

| Variable | Description | Default |
|---|---|---|
| `RELEASEA_DEBUG_CREDENTIALS` | Enables credential debug logs in API credential handlers | `false` |
| `WORKER_DEBUG_CREDENTIALS` | Compatibility debug flag shared with worker traces | `false` |

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
