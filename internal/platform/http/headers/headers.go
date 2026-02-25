package headers

import (
	"net/http"
	"strings"
)

const (
	HeaderAuthorization = "Authorization"
	HeaderContentType   = "Content-Type"
	HeaderAccept        = "Accept"
	HeaderUserAgent     = "User-Agent"

	ContentTypeJSON           = "application/json"
	ContentTypeFormURLEncoded = "application/x-www-form-urlencoded"
	AcceptJSON                = "application/json"
	AcceptGitHubJSON          = "application/vnd.github+json"

	DefaultUserAgent = "releasea-api"
)

func SetContentTypeJSON(req *http.Request) {
	if req == nil {
		return
	}
	req.Header.Set(HeaderContentType, ContentTypeJSON)
}

func SetContentTypeForm(req *http.Request) {
	if req == nil {
		return
	}
	req.Header.Set(HeaderContentType, ContentTypeFormURLEncoded)
}

func SetAcceptJSON(req *http.Request) {
	if req == nil {
		return
	}
	req.Header.Set(HeaderAccept, AcceptJSON)
}

func SetBearerToken(req *http.Request, token string) {
	if req == nil {
		return
	}
	req.Header.Set(HeaderAuthorization, "Bearer "+strings.TrimSpace(token))
}

func ApplyGitHubRequest(req *http.Request, token string, hasBody bool) {
	if req == nil {
		return
	}
	SetBearerToken(req, token)
	req.Header.Set(HeaderAccept, AcceptGitHubJSON)
	req.Header.Set(HeaderUserAgent, DefaultUserAgent)
	if hasBody {
		SetContentTypeJSON(req)
	}
}

func ExtractBearerToken(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	parts := strings.SplitN(trimmed, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

func ApplySSEResponse(h http.Header) {
	if h == nil {
		return
	}
	h.Set(HeaderContentType, "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}
