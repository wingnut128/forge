package attestation

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// DefaultRefreshInterval is the default interval between bundle fetches.
const DefaultRefreshInterval = 5 * time.Minute

// BundleRefresher fetches and caches a SPIFFE trust bundle from a remote
// bundle endpoint (RFC 9409), refreshing it on a fixed interval.
// It implements jwtbundle.Source for use with JWT-SVID validation.
type BundleRefresher struct {
	mu          sync.RWMutex
	bundle      *spiffebundle.Bundle
	trustDomain spiffeid.TrustDomain
	endpointURL string
	interval    time.Duration
	httpClient  *http.Client
}

// NewBundleRefresher creates a refresher for the given trust domain's bundle endpoint.
// If interval is zero or negative, DefaultRefreshInterval is used.
func NewBundleRefresher(trustDomain, endpointURL string, interval time.Duration) (*BundleRefresher, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid trust domain: %w", err)
	}
	if endpointURL == "" {
		return nil, fmt.Errorf("bundle endpoint URL is required")
	}
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	return &BundleRefresher{
		trustDomain: td,
		endpointURL: endpointURL,
		interval:    interval,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// fetch retrieves the bundle from the remote endpoint and updates the cache.
func (r *BundleRefresher) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpointURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching bundle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bundle endpoint returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	bundle, err := spiffebundle.Parse(r.trustDomain, data)
	if err != nil {
		return fmt.Errorf("parsing bundle: %w", err)
	}
	r.mu.Lock()
	r.bundle = bundle
	r.mu.Unlock()
	return nil
}

// Start performs an initial fetch and then refreshes the bundle in the background.
// The background goroutine stops when ctx is canceled.
func (r *BundleRefresher) Start(ctx context.Context) error {
	if err := r.fetch(ctx); err != nil {
		return fmt.Errorf("initial bundle fetch: %w", err)
	}
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.fetch(ctx); err != nil {
					log.Printf("bundle refresh failed for %s: %v", r.trustDomain, err)
				}
			}
		}
	}()
	return nil
}

// GetJWTBundleForTrustDomain returns the cached JWT bundle for the given trust domain.
// This implements jwtbundle.Source.
func (r *BundleRefresher) GetJWTBundleForTrustDomain(td spiffeid.TrustDomain) (*jwtbundle.Bundle, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.bundle == nil {
		return nil, fmt.Errorf("no bundle available for trust domain %q", td)
	}
	if td != r.trustDomain {
		return nil, fmt.Errorf("unknown trust domain %q (have %q)", td, r.trustDomain)
	}
	return r.bundle.JWTBundle(), nil
}
