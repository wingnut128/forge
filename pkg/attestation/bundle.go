package attestation

import (
	"context"
	"crypto"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// DefaultRefreshInterval is the default interval between bundle fetches.
const DefaultRefreshInterval = 5 * time.Minute

// minRefreshInterval floors the effective refresh cadence so a hostile or
// misconfigured refresh hint cannot make the refresher hammer the endpoint.
const minRefreshInterval = 1 * time.Minute

// BundleRefresher fetches and caches a SPIFFE trust bundle from a remote
// bundle endpoint (RFC 9409), refreshing it on a fixed interval.
// It implements jwtbundle.Source for use with JWT-SVID validation.
type BundleRefresher struct {
	mu          sync.RWMutex
	bundle      *spiffebundle.Bundle
	lastRefresh time.Time
	trustDomain spiffeid.TrustDomain
	endpointURL string
	interval    time.Duration
	httpClient  *http.Client
	logger      *slog.Logger
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
		logger:      slog.Default(),
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
	defer func() { _ = resp.Body.Close() }()
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
	// Continuity guard: never overwrite a working trust root with an empty
	// bundle. A fetch that yields no authorities is treated as a failure so the
	// previously cached bundle is retained rather than silently wiped.
	if bundle.JWTBundle().Empty() {
		return fmt.Errorf("refusing empty trust bundle for %q", r.trustDomain)
	}

	r.mu.Lock()
	prev := r.bundle
	r.bundle = bundle
	r.lastRefresh = time.Now()
	r.mu.Unlock()

	r.logRefresh(prev, bundle)
	return nil
}

// logRefresh records the outcome of a successful fetch, raising a warning when
// the set of signing authorities changes — i.e. the trust root rotated or was
// replaced — so a wholesale root swap is never silent.
func (r *BundleRefresher) logRefresh(prev, next *spiffebundle.Bundle) {
	nextAuth := next.JWTAuthorities()
	if prev == nil {
		r.logger.Info("trust bundle loaded",
			"trust_domain", r.trustDomain.Name(), "jwt_authorities", len(nextAuth))
		return
	}
	if !sameAuthorities(prev.JWTAuthorities(), nextAuth) {
		r.logger.Warn("trust root changed on refresh",
			"trust_domain", r.trustDomain.Name(),
			"previous_authorities", len(prev.JWTAuthorities()),
			"new_authorities", len(nextAuth))
	}
}

// sameAuthorities reports whether two authority sets carry the same key IDs.
func sameAuthorities(a, b map[string]crypto.PublicKey) bool {
	if len(a) != len(b) {
		return false
	}
	for kid := range a {
		if _, ok := b[kid]; !ok {
			return false
		}
	}
	return true
}

// Start performs an initial fetch and then refreshes the bundle in the background.
// The background goroutine stops when ctx is canceled.
func (r *BundleRefresher) Start(ctx context.Context) error {
	if err := r.fetch(ctx); err != nil {
		return fmt.Errorf("initial bundle fetch: %w", err)
	}
	go func() {
		timer := time.NewTimer(r.nextInterval())
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := r.fetch(ctx); err != nil {
					r.logger.Warn("bundle refresh failed",
						"trust_domain", r.trustDomain.Name(), "error", err)
				}
				timer.Reset(r.nextInterval())
			}
		}
	}()
	return nil
}

// nextInterval returns the cadence until the next refresh, honoring the
// bundle's RFC 9409 refresh hint when present, floored by minRefreshInterval.
func (r *BundleRefresher) nextInterval() time.Duration {
	interval := r.interval
	r.mu.RLock()
	if r.bundle != nil {
		if hint, ok := r.bundle.RefreshHint(); ok && hint > 0 {
			interval = hint
		}
	}
	r.mu.RUnlock()
	if interval < minRefreshInterval {
		interval = minRefreshInterval
	}
	return interval
}

// LastRefresh returns the time of the last successful bundle fetch, or the zero
// time if none has succeeded. Callers can use it to surface staleness.
func (r *BundleRefresher) LastRefresh() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastRefresh
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
