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
	"gitlab.com/cloudreaper/forge/pkg/attestation"
	"gitlab.com/cloudreaper/forge/pkg/authz"
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
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/healthz", s.handleHealthz)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
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

	resp := validateResponse{
		Valid:       true,
		SpiffeID:    svid.ID.String(),
		TrustDomain: svid.ID.TrustDomain().String(),
		Expiry:      svid.Expiry.Format(time.RFC3339),
	}

	if s.authorizer != nil && req.Action != "" && req.Resource != "" {
		decision, err := s.authorizer.IsAuthorized(svid.ID.String(), req.Action, req.Resource)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, validateResponse{Error: "authorization evaluation failed"})
			return
		}
		resp.Authorized = &decision.Allowed
		if !decision.Allowed {
			resp.DenyReason = decision.Reason
		}
	}

	writeJSON(w, http.StatusOK, resp)
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
	_ = json.NewEncoder(w).Encode(v)
}
