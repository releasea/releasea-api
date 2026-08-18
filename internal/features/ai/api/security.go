package ai

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)([^\s,;]+)`),
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)([^\s,;]+)`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|secret|private[_-]?key)\s*[=:]\s*)([^\s,;]+)`),
	regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._~+/-]+`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}\b`),
}

func redactSensitive(value string) string {
	redacted := value
	for _, pattern := range sensitivePatterns {
		redacted = pattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	}
	return redacted
}

func validateProviderURL(raw string, allowPrivate bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("base URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("base URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("base URL cannot include credentials, query, or fragment")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "metadata.google.internal" || host == "metadata" {
		return "", fmt.Errorf("metadata endpoints are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", fmt.Errorf("link-local, multicast, and unspecified addresses are not allowed")
		}
		if (ip.IsPrivate() || ip.IsLoopback()) && !allowPrivate {
			return "", fmt.Errorf("private network access must be explicitly enabled")
		}
	}
	if (host == "localhost" || strings.HasSuffix(host, ".local")) && !allowPrivate {
		return "", fmt.Errorf("private network access must be explicitly enabled")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func providerHTTPClient(provider providerConfig) *http.Client {
	dialer := &net.Dialer{Timeout: provider.Timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, resolved := range addresses {
				ip := resolved.IP
				if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					return nil, fmt.Errorf("provider resolved to a blocked network address")
				}
				if (ip.IsPrivate() || ip.IsLoopback()) && !provider.AllowPrivateNetwork {
					return nil, fmt.Errorf("provider resolved to a private network address")
				}
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("provider host did not resolve")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:       provider.Timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
