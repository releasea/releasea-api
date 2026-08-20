package observability

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryPrometheusRangeIgnoresNonFiniteSamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1,"NaN"],[2,"+Inf"],[3,"42.5"]]}]}}`)
	}))
	defer server.Close()

	samples, err := QueryPrometheusRange(
		context.Background(),
		server.URL,
		"up",
		time.Unix(1, 0),
		time.Unix(3, 0),
		time.Second,
	)
	if err != nil {
		t.Fatalf("query prometheus range: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want only the finite sample", len(samples))
	}
	if samples[0].Value != 42.5 {
		t.Fatalf("sample value = %v, want 42.5", samples[0].Value)
	}
}
