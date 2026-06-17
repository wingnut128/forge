package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/wingnut128/forge/pkg/attestation"
	"github.com/wingnut128/forge/pkg/authz"
)

// Request-handling guard rails. An unauthenticated caller can reach /validate,
// so cap how fast and how long a single request can consume the server.
const (
	requestTimeout    = 15 * time.Second
	defaultRatePerSec = 50
	defaultRateBurst  = 100
)

// Server is the attestation HTTP service that validates JWT-SVIDs
// from remote SPIFFE trust domains.
type Server struct {
	pair       *attestation.FederationPair
	bundles    jwtbundle.Source
	remoteTD   spiffeid.TrustDomain
	authorizer authz.Authorizer // nil means authorization disabled
	httpServer *http.Server
	listener   net.Listener
	logger     *slog.Logger
	limiter    *tokenBucket
}

// NewServer creates an attestation server that validates tokens against the
// given federation pair and bundle source.
func NewServer(pair *attestation.FederationPair, bundles jwtbundle.Source, addr string, authorizer authz.Authorizer) (*Server, error) {
	if pair == nil {
		return nil, fmt.Errorf("federation pair is required")
	}
	remoteTD, err := spiffeid.TrustDomainFromString(pair.Remote.Name)
	if err != nil {
		return nil, fmt.Errorf("remote trust domain: %w", err)
	}
	s := &Server{
		pair:       pair,
		bundles:    bundles,
		remoteTD:   remoteTD,
		authorizer: authorizer,
		logger:     slog.Default(),
		limiter:    newTokenBucket(defaultRatePerSec, defaultRateBurst),
	}
	mux := http.NewServeMux()
	mux.Handle("/validate", s.rateLimited(http.HandlerFunc(s.handleValidate)))
	mux.HandleFunc("/healthz", s.handleHealthz)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           http.TimeoutHandler(mux, requestTimeout, `{"error":"request timeout"}`),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// rateLimited rejects requests with 429 once the global token bucket is empty,
// bounding load from an unauthenticated caller.
func (s *Server) rateLimited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow() {
			writeJSON(w, http.StatusTooManyRequests, validateResponse{Error: "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Addr returns the listener's address. Only valid after Start is called.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.httpServer.Addr
}

// Start listens on the configured address and serves until ctx is canceled.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.listener = ln

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

type validateRequest struct {
	Token    string `json:"token"`
	Action   string `json:"action,omitempty"`
	Resource string `json:"resource,omitempty"`
}

// validateResponse reports the outcome of a /validate call.
//
// Contract: Valid means the SVID is authentic (signature, audience, trust
// domain) — it is NOT an access decision. Authorization is reported separately
// in Authorized, which is non-nil only when both action and resource were
// supplied and an authorizer is configured. A consumer must treat a missing
// Authorized as "not authorization-checked", never as "allowed".
type validateResponse struct {
	Valid       bool   `json:"valid"`
	SpiffeID    string `json:"spiffe_id,omitempty"`
	TrustDomain string `json:"trust_domain,omitempty"`
	Expiry      string `json:"expiry,omitempty"`
	Authorized  *bool  `json:"authorized,omitempty"`
	DenyReason  string `json:"deny_reason,omitempty"`
	Error       string `json:"error,omitempty"`
}

type healthResponse struct {
	Status      string `json:"status"`
	LastRefresh string `json:"last_refresh,omitempty"`
}

// lastRefresher is the optional capability a bundle source exposes to report
// when it last successfully refreshed, letting /healthz surface staleness.
type lastRefresher interface {
	LastRefresh() time.Time
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	var req validateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, validateResponse{Error: "invalid request body"})
		return
	}
	if req.Token == "" {
		writeJSON(w, http.StatusBadRequest, validateResponse{Error: "token is required"})
		return
	}

	svid, err := attestation.ValidateRemoteSVID(req.Token, s.pair, s.bundles)
	if err != nil {
		// Log the detail server-side; return a generic message so a caller
		// tuning a forged token learns nothing from the error text.
		s.logger.Warn("svid validation failed", "remote_addr", r.RemoteAddr, "error", err)
		writeJSON(w, http.StatusUnauthorized, validateResponse{Valid: false, Error: "token validation failed"})
		return
	}

	resp := validateResponse{
		Valid:       true,
		SpiffeID:    svid.ID.String(),
		TrustDomain: svid.ID.TrustDomain().String(),
		Expiry:      svid.Expiry.Format(time.RFC3339),
	}

	if s.authorizer != nil && (req.Action != "" || req.Resource != "") {
		// Fail closed on a partial authorization request: silently skipping
		// authz when only one of action/resource is present would let a caller
		// dodge the policy gate.
		if req.Action == "" || req.Resource == "" {
			writeJSON(w, http.StatusBadRequest, validateResponse{
				Error: "action and resource must both be provided for authorization",
			})
			return
		}
		decision, err := s.authorizer.IsAuthorized(svid.ID.String(), req.Action, req.Resource)
		if err != nil {
			s.logger.Error("authorization evaluation failed",
				"spiffe_id", svid.ID.String(), "action", req.Action, "resource", req.Resource, "error", err)
			writeJSON(w, http.StatusInternalServerError, validateResponse{Error: "authorization evaluation failed"})
			return
		}
		resp.Authorized = &decision.Allowed
		if !decision.Allowed {
			// Log the policy detail; return a generic reason to the caller.
			s.logger.Info("authorization denied",
				"spiffe_id", svid.ID.String(), "action", req.Action, "resource", req.Resource, "reason", decision.Reason)
			resp.DenyReason = "request not authorized"
		}
	}

	s.logger.Info("svid validated",
		"spiffe_id", svid.ID.String(), "trust_domain", svid.ID.TrustDomain().String(),
		"authorized", resp.Authorized, "remote_addr", r.RemoteAddr)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, err := s.bundles.GetJWTBundleForTrustDomain(s.remoteTD)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "no bundle loaded"})
		return
	}
	resp := healthResponse{Status: "ok"}
	if lr, ok := s.bundles.(lastRefresher); ok {
		if t := lr.LastRefresh(); !t.IsZero() {
			resp.LastRefresh = t.Format(time.RFC3339)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// tokenBucket is a minimal global rate limiter: it refills at ratePerSec tokens
// per second up to burst, and allow() spends one token per request.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	burst      float64
	ratePerSec float64
	last       time.Time
}

func newTokenBucket(ratePerSec, burst float64) *tokenBucket {
	return &tokenBucket{
		tokens:     burst,
		burst:      burst,
		ratePerSec: ratePerSec,
		last:       time.Now(),
	}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.ratePerSec
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
