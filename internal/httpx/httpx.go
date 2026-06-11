// Package httpx defines the minimal HTTP seam Hangar's fetchers depend on, so
// network clients can be mocked in tests.
package httpx

import (
	"net/http"
	"time"
)

// Doer performs an HTTP request. *http.Client satisfies it; tests provide fakes.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Default is the client used when a fetcher is constructed without one. It has
// no overall timeout; callers control deadlines via request contexts so large
// tarball downloads are not cut short.
var Default Doer = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}
