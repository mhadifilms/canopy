// Package api implements the coordination server HTTP API.
// Endpoints: /v1/checkin, /v1/endpoints, /v1/register_pairing, /v1/push
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/canopy-dev/coord/internal/auth"
	"github.com/canopy-dev/coord/internal/ratelimit"
	"github.com/canopy-dev/coord/internal/store"
)

// Server is the coordination API server.
type Server struct {
	store         *store.Store
	logger        *zap.Logger
	checkinLimiter *ratelimit.Limiter // 100 check-ins/min per device
	pushLimiter   *ratelimit.Limiter  // 30 pushes/min per device
	mux           *http.ServeMux
}

// New creates a new API server.
func New(s *store.Store, logger *zap.Logger) *Server {
	srv := &Server{
		store:          s,
		logger:         logger,
		checkinLimiter: ratelimit.New(100.0/60.0, 10), // ~1.67/s, burst 10
		pushLimiter:    ratelimit.New(30.0/60.0, 5),    // 0.5/s, burst 5
		mux:            http.NewServeMux(),
	}

	srv.mux.HandleFunc("POST /v1/checkin", srv.handleCheckin)
	srv.mux.HandleFunc("GET /v1/endpoints", srv.handleEndpoints)
	srv.mux.HandleFunc("POST /v1/register_pairing", srv.handleRegisterPairing)
	srv.mux.HandleFunc("POST /v1/push", srv.handlePush)
	srv.mux.HandleFunc("GET /healthz", srv.handleHealth)

	return srv
}

// Handler returns the HTTP handler for the API.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// --- Request/Response types ---

// CheckinRequest matches the daemon's check-in payload.
type CheckinRequest struct {
	DeviceKey     string           `json:"device_key"`
	WGPublicKey   string           `json:"wg_public_key"`
	Endpoints     []store.Endpoint `json:"endpoints"`
	PairedDevices []string         `json:"paired_devices"`
	APNSTokens    []string         `json:"apns_tokens"`
	Timestamp     string           `json:"timestamp"`
	Sig           string           `json:"sig"`
}

// CheckinResponse is returned on successful check-in.
type CheckinResponse struct {
	OK       bool   `json:"ok"`
	DeviceID string `json:"device_id"`
}

// EndpointsResponse is returned for endpoint lookups.
type EndpointsResponse struct {
	Endpoints []store.Endpoint `json:"endpoints"`
	Online    bool             `json:"online"`
}

// RegisterPairingRequest matches the pairing registration payload.
type RegisterPairingRequest struct {
	DeviceKey string `json:"device_key"`
	PeerWGKey string `json:"peer_wg_key"`
	Sig       string `json:"sig"`
}

// PushTarget describes a single push notification target.
type PushTarget struct {
	APNSToken    string       `json:"apns_token"`
	Notification Notification `json:"notification"`
}

// Notification is the push notification payload.
type Notification struct {
	Title    string            `json:"title"`
	Subtitle string           `json:"subtitle"`
	Body     string            `json:"body"`
	Category string            `json:"category"`
	ThreadID string            `json:"thread_id"`
	Data     map[string]string `json:"data"`
}

// PushRequest matches the daemon's push trigger payload.
type PushRequest struct {
	DeviceKey string       `json:"device_key"`
	Sig       string       `json:"sig"`
	Targets   []PushTarget `json:"targets"`
}

// ErrorResponse is returned on errors.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// --- Handlers ---

func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	var req CheckinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.DeviceKey == "" || req.WGPublicKey == "" {
		s.writeError(w, http.StatusBadRequest, "missing_fields", "device_key and wg_public_key required")
		return
	}

	// Rate limit.
	if !s.checkinLimiter.Allow(req.DeviceKey) {
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many check-ins")
		return
	}

	// Verify timestamp.
	if err := auth.ValidateTimestamp(req.Timestamp); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_timestamp", err.Error())
		return
	}

	// Verify signature: sign over the JSON body minus the "sig" field.
	// For simplicity, we verify the signature over a canonical representation
	// of the signed fields: device_key + wg_public_key + timestamp.
	signedMessage := req.DeviceKey + req.WGPublicKey + req.Timestamp
	if err := auth.VerifySignature(req.DeviceKey, req.Sig, []byte(signedMessage)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	// Store the registration.
	s.store.Checkin(req.DeviceKey, req.WGPublicKey, req.Endpoints, req.PairedDevices, req.APNSTokens)

	deviceID, _ := auth.DeviceIDFromPublicKey(req.DeviceKey)

	s.logger.Info("device checked in",
		zap.String("device_id", deviceID),
		zap.Int("endpoints", len(req.Endpoints)),
	)

	s.writeJSON(w, http.StatusOK, CheckinResponse{OK: true, DeviceID: deviceID})
}

func (s *Server) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	peerWGKey := r.URL.Query().Get("peer_wg_key")
	if peerWGKey == "" {
		s.writeError(w, http.StatusBadRequest, "missing_param", "peer_wg_key query parameter required")
		return
	}

	// Authenticate the requester via Bearer token.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		s.writeError(w, http.StatusUnauthorized, "missing_auth", "Authorization: Bearer <token> required")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	requesterKey, err := auth.VerifyBearerToken(token)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_token", err.Error())
		return
	}

	// Check pairing authorization.
	if !s.store.CanLookup(requesterKey, peerWGKey) {
		s.writeError(w, http.StatusForbidden, "not_paired", "no pairing registered between devices")
		return
	}

	// Look up the peer.
	rec := s.store.LookupByWGKey(peerWGKey)
	if rec == nil {
		s.writeJSON(w, http.StatusOK, EndpointsResponse{Endpoints: []store.Endpoint{}, Online: false})
		return
	}

	online := s.store.IsOnline(peerWGKey)
	s.writeJSON(w, http.StatusOK, EndpointsResponse{Endpoints: rec.Endpoints, Online: online})
}

func (s *Server) handleRegisterPairing(w http.ResponseWriter, r *http.Request) {
	var req RegisterPairingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.DeviceKey == "" || req.PeerWGKey == "" {
		s.writeError(w, http.StatusBadRequest, "missing_fields", "device_key and peer_wg_key required")
		return
	}

	// Verify signature over device_key + peer_wg_key.
	signedMessage := req.DeviceKey + req.PeerWGKey
	if err := auth.VerifySignature(req.DeviceKey, req.Sig, []byte(signedMessage)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	s.store.RegisterPairing(req.DeviceKey, req.PeerWGKey)

	deviceID, _ := auth.DeviceIDFromPublicKey(req.DeviceKey)
	s.logger.Info("pairing registered",
		zap.String("device_id", deviceID),
		zap.String("peer_wg_key", req.PeerWGKey[:8]+"..."),
	)

	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	var req PushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.DeviceKey == "" {
		s.writeError(w, http.StatusBadRequest, "missing_fields", "device_key required")
		return
	}

	// Rate limit.
	if !s.pushLimiter.Allow(req.DeviceKey) {
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "too many push requests")
		return
	}

	// Verify signature over device_key.
	if err := auth.VerifySignature(req.DeviceKey, req.Sig, []byte(req.DeviceKey)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	if len(req.Targets) == 0 {
		s.writeError(w, http.StatusBadRequest, "no_targets", "at least one target required")
		return
	}

	// In MVP, we log the push request. Real implementation would forward to APNs.
	deviceID, _ := auth.DeviceIDFromPublicKey(req.DeviceKey)
	s.logger.Info("push notification request",
		zap.String("device_id", deviceID),
		zap.Int("targets", len(req.Targets)),
	)

	// For each target, log the notification.
	sent := 0
	for _, target := range req.Targets {
		if target.APNSToken == "" {
			continue
		}
		s.logger.Info("push forwarded",
			zap.String("apns_token", target.APNSToken[:8]+"..."),
			zap.String("title", target.Notification.Title),
		)
		sent++
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"sent": sent,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"devices": s.store.DeviceCount(),
	})
}

// --- Helpers ---

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("write json response", zap.Error(err))
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, ErrorResponse{
		Error:   code,
		Message: fmt.Sprintf("%s", message),
	})
}
