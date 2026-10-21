package rules

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

var headerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func normalizeIPList(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "/") {
			ip := net.ParseIP(value)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address %q", value)
			}
			if ip.To4() != nil {
				value += "/32"
			} else {
				value += "/128"
			}
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q", value)
		}
		out = append(out, network.String())
	}
	if len(out) > 50 {
		return nil, errors.New("at most 50 IP addresses are allowed")
	}
	return shared.UniqueStrings(out), nil
}

func normalizeHeaderNames(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		header := strings.ToLower(strings.TrimSpace(value))
		if header == "" {
			continue
		}
		if !headerNamePattern.MatchString(header) {
			return nil, fmt.Errorf("header %q is not a valid HTTP header name", value)
		}
		out = append(out, header)
	}
	if len(out) > 20 {
		return nil, errors.New("at most 20 headers are allowed")
	}
	return shared.UniqueStrings(out), nil
}

func normalizeTokenList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return shared.UniqueStrings(out)
}

func normalizeCORS(input map[string]interface{}) (bson.M, error) {
	enabled := shared.BoolValue(input["enabled"])
	origins := normalizeTokenList(shared.ToStringSlice(input["origins"]))
	if len(origins) > 20 {
		return nil, errors.New("at most 20 CORS origins are allowed")
	}
	if enabled {
		for _, origin := range origins {
			if origin == "*" {
				continue
			}
			parsed, err := url.ParseRequestURI(origin)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return nil, fmt.Errorf("CORS origin %q must be * or an absolute http(s) URL", origin)
			}
		}
	}
	methods := make([]string, 0)
	for _, method := range shared.ToStringSlice(input["methods"]) {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method != "" {
			methods = append(methods, method)
		}
	}
	headers, err := normalizeHeaderNames(shared.ToStringSlice(input["headers"]))
	if err != nil {
		return nil, err
	}
	maxAge := shared.IntValue(input["maxAgeSeconds"])
	if maxAge <= 0 {
		maxAge = 600
	}
	if maxAge > 86400 {
		maxAge = 86400
	}
	return bson.M{
		"enabled":       enabled,
		"origins":       origins,
		"methods":       shared.UniqueStrings(methods),
		"headers":       headers,
		"maxAgeSeconds": maxAge,
	}, nil
}

func normalizeRateLimit(input map[string]interface{}) (bson.M, error) {
	requests := shared.IntValue(input["requests"])
	window := shared.IntValue(input["windowSeconds"])
	if requests < 0 || requests > 100000 {
		return nil, errors.New("rate limit must be between 0 and 100000 requests")
	}
	if requests == 0 {
		window = 0
	} else if window <= 0 {
		window = 60
	}
	if window > 3600 {
		return nil, errors.New("rate limit window must be 3600 seconds or less")
	}
	return bson.M{"requests": requests, "windowSeconds": window}, nil
}

func normalizeCircuitBreaker(input map[string]interface{}) (bson.M, error) {
	consecutive := shared.IntValue(input["consecutiveErrors"])
	if consecutive < 0 || consecutive > 20 {
		return nil, errors.New("circuit breaker consecutive errors must be between 0 and 20")
	}
	interval := shared.IntValue(input["intervalSeconds"])
	base := shared.IntValue(input["baseEjectionSeconds"])
	maxEject := shared.IntValue(input["maxEjectionPercent"])
	if consecutive == 0 {
		return bson.M{
			"consecutiveErrors":   0,
			"intervalSeconds":     0,
			"baseEjectionSeconds": 0,
			"maxEjectionPercent":  0,
		}, nil
	}
	if interval <= 0 {
		interval = 10
	}
	if base <= 0 {
		base = 30
	}
	if maxEject <= 0 || maxEject > 100 {
		maxEject = 50
	}
	if interval > 600 || base > 600 {
		return nil, errors.New("circuit breaker intervals must be 600 seconds or less")
	}
	return bson.M{
		"consecutiveErrors":   consecutive,
		"intervalSeconds":     interval,
		"baseEjectionSeconds": base,
		"maxEjectionPercent":  maxEject,
	}, nil
}
