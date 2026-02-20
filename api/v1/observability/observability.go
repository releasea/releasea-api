package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
)

type MetricsPayload struct {
	ServiceID   string              `json:"serviceId"`
	Environment string              `json:"environment,omitempty"`
	Namespace   string              `json:"namespace,omitempty"`
	Timestamps  []string            `json:"timestamps"`
	Cpu         []float64           `json:"cpu"`
	Memory      []float64           `json:"memory"`
	LatencyP95  []float64           `json:"latencyP95"`
	Requests    []float64           `json:"requests"`
	StatusCodes *StatusCodeSeries   `json:"statusCodes,omitempty"`
	Diagnostics *MetricsDiagnostics `json:"diagnostics,omitempty"`
}

type StatusCodeSeries struct {
	TwoXX  []float64 `json:"2xx"`
	FourXX []float64 `json:"4xx"`
	FiveXX []float64 `json:"5xx"`
}

type MetricsDiagnostics struct {
	PrometheusURL       string `json:"prometheusUrl,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	ServiceName         string `json:"serviceName,omitempty"`
	TimeRangeStart      string `json:"timeRangeStart,omitempty"`
	TimeRangeEnd        string `json:"timeRangeEnd,omitempty"`
	CpuQuerySuccess     bool   `json:"cpuQuerySuccess"`
	MemQuerySuccess     bool   `json:"memQuerySuccess"`
	LatencyQuerySuccess bool   `json:"latencyQuerySuccess"`
	ReqQuerySuccess     bool   `json:"reqQuerySuccess"`
	Error               string `json:"error,omitempty"`
}

type LogPayload struct {
	ID        string                 `json:"id"`
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	ServiceID string                 `json:"serviceId"`
	Pod       string                 `json:"pod,omitempty"`
	Container string                 `json:"container,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type LogsResponse struct {
	Logs        []LogPayload     `json:"logs"`
	Diagnostics *LogsDiagnostics `json:"diagnostics,omitempty"`
}

type LogsDiagnostics struct {
	LokiURL     string `json:"lokiUrl,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	Query       string `json:"query,omitempty"`
	Error       string `json:"error,omitempty"`
}

type promRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][]interface{} `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type PromSample struct {
	Time  time.Time
	Value float64
}

type lokiRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func PrometheusURL() string {
	value := strings.TrimSpace(os.Getenv("PROMETHEUS_URL"))
	if value != "" {
		return value
	}
	return "http://releasea-prometheus-kube-prometheus-prometheus.monitoring:9090"
}

func LokiURL() string {
	value := strings.TrimSpace(os.Getenv("LOKI_URL"))
	if value != "" {
		return value
	}
	return "http://releasea-loki.monitoring:3100"
}

// NamespaceForEnvironment is deprecated - use resolveAppNamespace from namespace.go.
func NamespaceForEnvironment(environment string) string {
	return shared.ResolveAppNamespace(environment)
}

// CheckPrometheusHealth verifies Prometheus connectivity
func checkPrometheusHealth(ctx context.Context) (bool, string) {
	promURL := PrometheusURL()
	if promURL == "" {
		return false, "PROMETHEUS_URL not configured"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, promURL+"/-/healthy", nil)
	if err != nil {
		return false, fmt.Sprintf("failed to create health check request: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("prometheus unreachable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return false, fmt.Sprintf("prometheus health check failed: %s", resp.Status)
	}
	return true, ""
}

// CheckLokiHealth verifies Loki connectivity
func checkLokiHealth(ctx context.Context) (bool, string) {
	lokiURL := LokiURL()
	if lokiURL == "" {
		return false, "LOKI_URL not configured"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lokiURL+"/ready", nil)
	if err != nil {
		return false, fmt.Sprintf("failed to create health check request: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("loki unreachable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return false, fmt.Sprintf("loki health check failed: %s", resp.Status)
	}
	return true, ""
}

// KubeName delegates to toKubeName (defined in deploy_resources.go).
func KubeName(value string) string {
	return shared.ToKubeName(value)
}

func ParseMetricsRange(fromRaw, toRaw string) (time.Time, time.Time, time.Duration) {
	now := time.Now().UTC()
	end := now
	start := now.Add(-1 * time.Hour)
	if fromRaw != "" {
		if parsed, err := time.Parse(time.RFC3339, fromRaw); err == nil {
			start = parsed.UTC()
		}
	}
	if toRaw != "" {
		if parsed, err := time.Parse(time.RFC3339, toRaw); err == nil {
			end = parsed.UTC()
		}
	}
	if end.Before(start) {
		start, end = end, start
	}
	rangeSeconds := end.Sub(start).Seconds()
	step := time.Duration(math.Max(60, math.Floor(rangeSeconds/60))) * time.Second
	if step <= 0 {
		step = time.Minute
	}
	return start, end, step
}

func BuildTimestamps(start, end time.Time, step time.Duration) []time.Time {
	if step <= 0 {
		step = time.Minute
	}
	out := []time.Time{}
	cursor := start
	for !cursor.After(end) {
		out = append(out, cursor)
		cursor = cursor.Add(step)
	}
	if len(out) == 0 {
		out = append(out, start)
	}
	return out
}

func QueryPrometheusRange(ctx context.Context, baseURL, query string, start, end time.Time, step time.Duration) ([]PromSample, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("prometheus url missing")
	}
	values := url.Values{}
	values.Set("query", query)
	values.Set("start", fmt.Sprintf("%f", float64(start.Unix())))
	values.Set("end", fmt.Sprintf("%f", float64(end.Unix())))
	values.Set("step", fmt.Sprintf("%f", step.Seconds()))
	reqURL := baseURL + "/api/v1/query_range?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("prometheus query failed: %s", resp.Status)
	}
	var payload promRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" || len(payload.Data.Result) == 0 {
		return nil, nil
	}
	samples := make([]PromSample, 0)
	for _, entry := range payload.Data.Result {
		for _, value := range entry.Values {
			if len(value) < 2 {
				continue
			}
			ts, ok := value[0].(float64)
			if !ok {
				continue
			}
			rawValue, ok := value[1].(string)
			if !ok {
				continue
			}
			parsed, err := strconv.ParseFloat(rawValue, 64)
			if err != nil {
				continue
			}
			samples = append(samples, PromSample{
				Time:  time.Unix(int64(ts), 0).UTC(),
				Value: parsed,
			})
		}
	}
	return samples, nil
}

func FillSeries(samples []PromSample, start time.Time, step time.Duration, size int) []float64 {
	out := make([]float64, size)
	if len(samples) == 0 || size == 0 {
		return out
	}
	stepSeconds := step.Seconds()
	if stepSeconds <= 0 {
		stepSeconds = 60
	}
	for _, sample := range samples {
		index := int(math.Round(sample.Time.Sub(start).Seconds() / stepSeconds))
		if index < 0 || index >= size {
			continue
		}
		out[index] = sample.Value
	}
	return out
}

func QueryLokiRange(ctx context.Context, baseURL, query string, start, end time.Time, limit int) ([]LogPayload, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("loki url missing")
	}
	if limit <= 0 {
		limit = 200
	}
	values := url.Values{}
	values.Set("query", query)
	values.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	values.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	values.Set("limit", strconv.Itoa(limit))
	values.Set("direction", "BACKWARD")
	reqURL := baseURL + "/loki/api/v1/query_range?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("loki query failed: %s", resp.Status)
	}
	var payload lokiRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" {
		return nil, nil
	}
	entries := make([]LogPayload, 0)
	for _, stream := range payload.Data.Result {
		podName := stream.Stream["pod"]
		containerName := stream.Stream["container"]
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			tsNano, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				continue
			}
			message := value[1]
			ts := time.Unix(0, tsNano).UTC()
			metadata := map[string]interface{}{}
			if podName != "" {
				metadata["replicaName"] = podName
			}
			if containerName != "" {
				metadata["container"] = containerName
			}
			entries = append(entries, LogPayload{
				ID:        fmt.Sprintf("log-%d-%s", tsNano, podName),
				Timestamp: ts.Format(time.RFC3339Nano),
				Level:     detectLogLevel(message),
				Message:   message,
				Metadata:  metadata,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})
	return entries, nil
}

func detectLogLevel(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "panic"):
		return "error"
	case strings.Contains(lower, "error"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warn"
	case strings.Contains(lower, "debug"):
		return "debug"
	default:
		return "info"
	}
}

// ObservabilityHealth returns health status of Prometheus and Loki backends
func ObservabilityHealth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	promHealthy, promError := checkPrometheusHealth(ctx)
	lokiHealthy, lokiError := checkLokiHealth(ctx)

	status := "healthy"
	if !promHealthy || !lokiHealthy {
		status = "degraded"
	}
	if !promHealthy && !lokiHealthy {
		status = "unhealthy"
	}

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"prometheus": gin.H{
			"healthy": promHealthy,
			"url":     PrometheusURL(),
			"error":   promError,
		},
		"loki": gin.H{
			"healthy": lokiHealthy,
			"url":     LokiURL(),
			"error":   lokiError,
		},
	})
}
