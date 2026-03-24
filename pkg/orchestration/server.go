package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/wingnut128/forge/pkg/attestation"
)

// Server is the attestation HTTP service that validates JWT-SVIDs
// from remote SPIFFE trust domains.
type Server struct {
	pair       *attestation.FederationPair
	bundles    jwtbundle.Source
	remoteTD   spiffeid.TrustDomain
	httpServer *http.Server
	listener   net.Listener
}

// NewServer creates an attestation server that validates tokens against the
// given federation pair and bundle source.
func NewServer(pair *attestation.FederationPair, bundles jwtbundle.Source, addr string) *Server {
	s := &Server{
		pair:     pair,
		bundles:  bundles,
		remoteTD: spiffeid.RequireTrustDomainFromString(pair.Remote.Name),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/healthz", s.handleHealthz)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
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
	Token string `json:"token"`
}

type validateResponse struct {
	Valid       bool   `json:"valid"`
	SpiffeID    string `json:"spiffe_id,omitempty"`
	TrustDomain string `json:"trust_domain,omitempty"`
	Expiry      string `json:"expiry,omitempty"`
	Error       string `json:"error,omitempty"`
}

type healthResponse struct {
	Status string `json:"status"`
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
		writeJSON(w, http.StatusUnauthorized, validateResponse{Valid: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, validateResponse{
		Valid:       true,
		SpiffeID:    svid.ID.String(),
		TrustDomain: svid.ID.TrustDomain().String(),
		Expiry:      svid.Expiry.Format(time.RFC3339),
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, err := s.bundles.GetJWTBundleForTrustDomain(s.remoteTD)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "no bundle loaded"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
