FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN arch="${TARGETARCH:-}"; \
    if [ -z "${arch}" ]; then \
      case "$(uname -m)" in \
        aarch64|arm64) arch="arm64" ;; \
        x86_64|amd64) arch="amd64" ;; \
        *) arch="$(go env GOARCH)" ;; \
      esac; \
    fi; \
    CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${arch}" \
      go build -trimpath -ldflags="-s -w" -o /out/releasea-api ./cmd/main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/releasea-api ./releasea-api
EXPOSE 8070
ENV PORT=8070
CMD ["./releasea-api"]
