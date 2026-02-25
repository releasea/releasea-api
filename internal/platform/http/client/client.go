package httpclient

import (
	"net/http"
	"time"
)

const DefaultTimeout = 20 * time.Second

func New(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

func Default() *http.Client {
	return New(DefaultTimeout)
}
