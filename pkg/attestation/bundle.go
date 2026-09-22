package attestation

import (
	"context"
	"crypto"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/federation"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// DefaultRefreshInterval is the default interval between bundle fetches.
const DefaultRefreshInterval = 5 * time.Minute

// minRefreshInterval floors the effective refresh cadence so a hostile or
// misconfigured refresh hint cannot make the refresher hammer the endpoint.
const minRefreshInterval = 1 * time.Minute

// fetchTimeout bounds a single bundle fetch.
const fetchTimeout = 30 * time.Second

// BundleRefresher fetches and caches a SPIFFE trust bundle from a remote
// bundle endpoint (RFC 9409), refreshing it on a fixed interval.
//
// The endpoint is authenticated with the https_spiffe profile: it must present
// the peer SPIRE server's SVID, verified against the X.509 authorities of the
// currently cached bundle. The cache starts from a seed bundle — the one the
// bootstrap exchanged out of band — and each successful fetch becomes the
// trust root for the next, so authority rotation is followed.
//
// It implements jwtbundle.Source for use with JWT-SVID validation.
type BundleRefresher struct {
	mu          sync.RWMutex
	bundle      *spiffebundle.Bundle
	lastRefresh time.Time
	trustDomain spiffeid.TrustDomain
	endpointURL string
	endpointID  spiffeid.ID
	interval    time.Duration
	logger      *slog.Logger
}

// LoadSeedBundle reads a SPIFFE-format bundle for trustDomain from path — the
// output of `spire-server bundle show -format spiffe` on the peer.
func LoadSeedBundle(trustDomain, path string) (*spiffebundle.Bundle, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid trust domain: %w", err)
	}
	b, err := spiffebundle.Load(td, path)
	if err != nil {
		return nil, fmt.Errorf("loading seed bundle: %w", err)
	}
	return b, nil
}

// NewBundleRefresher creates a refresher for seed's trust domain, fetching from
// endpointURL. The endpoint must present spiffe://<trust-domain>/spire/server.
// If interval is zero or negative, DefaultRefreshInterval is used.
func NewBundleRefresher(seed *spiffebundle.Bundle, endpointURL string, interval time.Duration) (*BundleRefresher, error) {
	if seed == nil {
		return nil, fmt.Errorf("seed bundle is required")
	}
	if len(seed.X509Authorities()) == 0 {
		return nil, fmt.Errorf("seed bundle for %q has no X.509 authorities to authenticate the endpoint", seed.TrustDomain())
	}
	if endpointURL == "" {
		return nil, fmt.Errorf("bundle endpoint URL is required")
	}
	td := seed.TrustDomain()
	endpointID, err := spiffeid.FromPath(td, "/spire/server")
	if err != nil {
		return nil, fmt.Errorf("endpoint SPIFFE ID: %w", err)
	}
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	return &BundleRefresher{
		bundle:      seed,
		trustDomain: td,
		endpointURL: endpointURL,
		endpointID:  endpointID,
		interval:    interval,
		logger:      slog.Default(),
	}, nil
}

// fetch retrieves the bundle from the remote endpoint and updates the cache.
func (r *BundleRefresher) fetch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	bundle, err := federation.FetchBundle(ctx, r.trustDomain, r.endpointURL,
		federation.WithSPIFFEAuth(r, r.endpointID))
	if err != nil {
		return fmt.Errorf("fetching bundle: %w", err)
	}
	// Continuity guard: never overwrite a working trust root with an empty
	// bundle. A fetch that yields no authorities is treated as a failure so the
	// previously cached bundle is retained rather than silently wiped.
	if bundle.JWTBundle().Empty() {
		return fmt.Errorf("refusing empty trust bundle for %q", r.trustDomain)
	}
	// Without X.509 authorities the next fetch could not authenticate the
	// endpoint, so accepting this bundle would permanently break refresh.
	if len(bundle.X509Authorities()) == 0 {
		return fmt.Errorf("refusing trust bundle for %q with no X.509 authorities", r.trustDomain)
	}

	r.mu.Lock()
	prev, first := r.bundle, r.lastRefresh.IsZero()
	r.bundle = bundle
	r.lastRefresh = time.Now()
	r.mu.Unlock()

	r.logRefresh(first, prev, bundle)
	return nil
}

// logRefresh records the outcome of a successful fetch, raising a warning when
// the set of signing authorities changes — i.e. the trust root rotated or was
// replaced — so a wholesale root swap is never silent. On the first fetch prev
// is the seed, so a warning there means the peer rotated since the exchange.
func (r *BundleRefresher) logRefresh(first bool, prev, next *spiffebundle.Bundle) {
	nextAuth := next.JWTAuthorities()
	if first {
		r.logger.Info("trust bundle loaded",
			"trust_domain", r.trustDomain.Name(), "jwt_authorities", len(nextAuth))
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
	if hint, ok := r.bundle.RefreshHint(); ok && hint > 0 {
		interval = hint
	}
	r.mu.RUnlock()
	if interval < minRefreshInterval {
		interval = minRefreshInterval
	}
	return interval
}

// LastRefresh returns the time of the last successful bundle fetch, or the zero
// time if none has succeeded (the seed bundle does not count). Callers can use it to surface staleness.
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
	if td != r.trustDomain {
		return nil, fmt.Errorf("unknown trust domain %q (have %q)", td, r.trustDomain)
	}
	return r.bundle.JWTBundle(), nil
}

// GetX509BundleForTrustDomain returns the cached X.509 authorities for the
// given trust domain. This implements x509bundle.Source, so the refresher
// authenticates each fetch against the bundle it currently trusts.
func (r *BundleRefresher) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if td != r.trustDomain {
		return nil, fmt.Errorf("unknown trust domain %q (have %q)", td, r.trustDomain)
	}
	return r.bundle.X509Bundle(), nil
}
