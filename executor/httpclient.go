package executor

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// ClientConfig is the HTTP client configuration shared by every executor.
//
// A comparison between variants is only meaningful if every variant is driven
// with the same offered load. Previously it was not: the Hades executor used
// http.DefaultClient (no timeout at all, and MaxIdleConnsPerHost of 2, so all
// but two submissions paid a fresh TCP and TLS handshake), while the Jenkins
// executor used a private client with a 10s timeout and MaxConnsPerHost of 100.
//
// MaxConnsPerHost is the worst of those: it is a hard cap, so submission 101
// blocks waiting for a free connection, and that wait counts against the
// client timeout. Under load the Jenkins variant therefore both throttled
// itself and started failing submissions for a reason that had nothing to do
// with Jenkins.
type ClientConfig struct {
	// Timeout bounds the whole submission round-trip.
	Timeout time.Duration
	// DialTimeout bounds connection establishment.
	DialTimeout time.Duration
	// MaxIdleConns and MaxIdleConnsPerHost keep connections warm so that
	// submission cost measures the system under test, not TLS handshakes.
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	// MaxConnsPerHost is deliberately 0, meaning unlimited. Concurrency is
	// shaped explicitly by the benchmark pacer, never accidentally by the
	// connection pool.
	MaxConnsPerHost int
	IdleConnTimeout time.Duration
}

// DefaultClientConfig returns the configuration every executor uses unless a
// test overrides it.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Timeout:             30 * time.Second,
		DialTimeout:         10 * time.Second,
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 1024,
		MaxConnsPerHost:     0,
		IdleConnTimeout:     90 * time.Second,
	}
}

// NewClient builds an HTTP client from cfg.
func NewClient(cfg ClientConfig) *http.Client {
	return &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   cfg.DialTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          cfg.MaxIdleConns,
			MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
			MaxConnsPerHost:       cfg.MaxConnsPerHost,
			IdleConnTimeout:       cfg.IdleConnTimeout,
			TLSHandshakeTimeout:   cfg.DialTimeout,
			ExpectContinueTimeout: time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

var (
	sharedOnce   sync.Once
	sharedClient *http.Client
	sharedConfig = DefaultClientConfig()
	configMu     sync.Mutex
)

// ConfigureSharedClient replaces the shared configuration. It must be called
// before the first SharedClient call, i.e. at startup.
func ConfigureSharedClient(cfg ClientConfig) {
	configMu.Lock()
	defer configMu.Unlock()
	sharedConfig = cfg
}

// SharedClient returns the process-wide HTTP client. Every executor uses it, so
// every variant sees identical timeout, dial and connection-pool behaviour.
func SharedClient() *http.Client {
	sharedOnce.Do(func() {
		configMu.Lock()
		cfg := sharedConfig
		configMu.Unlock()
		sharedClient = NewClient(cfg)
	})
	return sharedClient
}
