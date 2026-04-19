// Package api implements the coordination server HTTP API.
// Endpoints: /v1/checkin, /v1/endpoints, /v1/register_pairing, /v1/push, /v1/pairing
package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/canopy-dev/coord/internal/apns"
	"github.com/canopy-dev/coord/internal/auth"
	"github.com/canopy-dev/coord/internal/ratelimit"
	"github.com/canopy-dev/coord/internal/store"
)

// maxRequestBody caps JSON bodies at 64 KiB. Our largest request (check-in
// with endpoints + paired keys) is a few KB in practice, so this leaves
// generous headroom while rejecting obvious DoS payloads at the edge.
const maxRequestBody = 64 * 1024

var pairingCodeRe = regexp.MustCompile(`^/v1/pairing/([0-9]{6})/status$`)

// Server is the coordination API server.
type Server struct {
	store          *store.Store
	logger         *zap.Logger
	checkinLimiter *ratelimit.Limiter // 100 check-ins/min per device
	pushLimiter    *ratelimit.Limiter // 30 pushes/min per device
	apnsClient     *apns.Client
	mux            *http.ServeMux
}

// New creates a new API server.
func New(s *store.Store, logger *zap.Logger) *Server {
	apnsClient := apns.NewClient()
	if apnsClient != nil {
		logger.Info("APNs client initialized, push notifications will be forwarded")
	} else {
		logger.Info("APNs client not configured, push notifications will be logged only")
	}

	srv := &Server{
		store:          s,
		logger:         logger,
		checkinLimiter: ratelimit.New(100.0/60.0, 10), // ~1.67/s, burst 10
		pushLimiter:    ratelimit.New(30.0/60.0, 5),    // 0.5/s, burst 5
		apnsClient:     apnsClient,
		mux:            http.NewServeMux(),
	}

	srv.mux.HandleFunc("POST /v1/checkin", srv.handleCheckin)
	srv.mux.HandleFunc("GET /v1/endpoints", srv.handleEndpoints)
	srv.mux.HandleFunc("POST /v1/register_pairing", srv.handleRegisterPairing)
	srv.mux.HandleFunc("POST /v1/push", srv.handlePush)
	srv.mux.HandleFunc("POST /v1/pairing/initiate", srv.handlePairingInitiate)
	srv.mux.HandleFunc("POST /v1/pairing/confirm", srv.handlePairingConfirm)
	srv.mux.HandleFunc("GET /v1/pairing/", srv.handlePairingStatus)
	srv.mux.HandleFunc("GET /v1/stun", srv.handleSTUN)
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

// PairingInitiateRequest is sent by the Mac daemon when starting `canopyd pair`.
type PairingInitiateRequest struct {
	Code     string `json:"code"`
	Hostname string `json:"hostname"`
	DeviceID string `json:"device_id"`
	WGPub    string `json:"wg_pub"`
	Identity string `json:"identity"`
	Sig      string `json:"sig"`
}

// PairingConfirmRequest is sent by the Mac daemon to confirm a pairing.
type PairingConfirmRequest struct {
	Code     string `json:"code"`
	Hostname string `json:"hostname"`
	DeviceID string `json:"device_id"`
	WGPub    string `json:"wg_pub"`
	Identity string `json:"identity"`
	Sig      string `json:"sig"`
}

// PairingStatusResponse is returned when polling for pairing status.
type PairingStatusResponse struct {
	Status   string `json:"status"`
	Hostname string `json:"hostname,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	WGPub    string `json:"wg_pub,omitempty"`
	Identity string `json:"identity,omitempty"`
}

// ErrorResponse is returned on errors.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// --- Handlers ---

func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	var req CheckinRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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

	// Verify signature over a canonical message with explicit newline
	// separators so adjacent field contents cannot collide across boundaries.
	if err := auth.VerifySignature(req.DeviceKey, req.Sig, auth.CanonicalCheckinMessage(req.DeviceKey, req.WGPublicKey, req.Timestamp)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	// Store the registration.
	s.store.Checkin(req.DeviceKey, req.WGPublicKey, req.Endpoints, req.PairedDevices, req.APNSTokens)

	deviceID, err := auth.DeviceIDFromPublicKey(req.DeviceKey)
	if err != nil {
		s.logger.Warn("derive device id for checkin", zap.Error(err))
	}

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
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.DeviceKey == "" || req.PeerWGKey == "" {
		s.writeError(w, http.StatusBadRequest, "missing_fields", "device_key and peer_wg_key required")
		return
	}

	if err := auth.VerifySignature(req.DeviceKey, req.Sig, auth.CanonicalRegisterPairingMessage(req.DeviceKey, req.PeerWGKey)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	s.store.RegisterPairing(req.DeviceKey, req.PeerWGKey)

	deviceID, err := auth.DeviceIDFromPublicKey(req.DeviceKey)
	if err != nil {
		s.logger.Warn("derive device id for registered pairing", zap.Error(err))
	}
	s.logger.Info("pairing registered",
		zap.String("device_id", deviceID),
		zap.String("peer_wg_key", shortKey(req.PeerWGKey)),
	)

	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	var req PushRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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

	if err := auth.VerifySignature(req.DeviceKey, req.Sig, auth.CanonicalPushMessage(req.DeviceKey)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	if len(req.Targets) == 0 {
		s.writeError(w, http.StatusBadRequest, "no_targets", "at least one target required")
		return
	}

	deviceID, err := auth.DeviceIDFromPublicKey(req.DeviceKey)
	if err != nil {
		s.logger.Warn("derive device id for push", zap.Error(err))
	}
	s.logger.Info("push notification request",
		zap.String("device_id", deviceID),
		zap.Int("targets", len(req.Targets)),
	)

	sent := 0
	failed := 0
	for _, target := range req.Targets {
		if target.APNSToken == "" {
			continue
		}

		if s.apnsClient != nil {
			payload := notificationToPayload(target.Notification)
			if err := s.apnsClient.Send(r.Context(), target.APNSToken, payload); err != nil {
				s.logger.Warn("APNs push failed",
					zap.String("apns_token", shortKey(target.APNSToken)),
					zap.Error(err),
				)
				failed++
				continue
			}
		}

		s.logger.Info("push forwarded",
			zap.String("apns_token", shortKey(target.APNSToken)),
			zap.String("title", target.Notification.Title),
		)
		sent++
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":     true,
		"sent":   sent,
		"failed": failed,
	})
}

func (s *Server) handlePairingInitiate(w http.ResponseWriter, r *http.Request) {
	var req PairingInitiateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.Code == "" || req.Identity == "" {
		s.writeError(w, http.StatusBadRequest, "missing_fields", "code and identity required")
		return
	}

	if err := auth.VerifySignature(req.Identity, req.Sig, auth.CanonicalPairingMessage("initiate", req.Code, req.Identity, req.WGPub)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	// Create the pairing session as pending first, then confirm it with the Mac's info.
	s.store.CreatePairingSession(req.Code)
	s.store.ConfirmPairingSession(req.Code, req.Hostname, req.DeviceID, req.WGPub, req.Identity)

	s.logger.Info("pairing initiated",
		zap.String("code", req.Code),
		zap.String("hostname", req.Hostname),
	)

	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePairingConfirm(w http.ResponseWriter, r *http.Request) {
	var req PairingConfirmRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	if req.Code == "" || req.Identity == "" {
		s.writeError(w, http.StatusBadRequest, "missing_fields", "code and identity required")
		return
	}

	if err := auth.VerifySignature(req.Identity, req.Sig, auth.CanonicalPairingMessage("confirm", req.Code, req.Identity, req.WGPub)); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}

	ok := s.store.ConfirmPairingSession(req.Code, req.Hostname, req.DeviceID, req.WGPub, req.Identity)
	if !ok {
		s.writeError(w, http.StatusNotFound, "not_found", "pairing session not found or expired")
		return
	}

	s.logger.Info("pairing confirmed",
		zap.String("code", req.Code),
		zap.String("hostname", req.Hostname),
	)

	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePairingStatus(w http.ResponseWriter, r *http.Request) {
	matches := pairingCodeRe.FindStringSubmatch(r.URL.Path)
	if matches == nil {
		s.writeError(w, http.StatusBadRequest, "invalid_path", "expected /v1/pairing/{6-digit-code}/status")
		return
	}
	code := matches[1]

	sess := s.store.GetPairingSession(code)
	if sess == nil {
		s.writeJSON(w, http.StatusOK, PairingStatusResponse{Status: "pending"})
		return
	}

	resp := PairingStatusResponse{Status: sess.Status}
	if sess.Status == "confirmed" {
		resp.Hostname = sess.Hostname
		resp.DeviceID = sess.DeviceID
		resp.WGPub = sess.WGPub
		resp.Identity = sess.Identity
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleSTUN reflects the caller's observed address back as JSON. This is a
// lightweight HTTP-based replacement for RFC 5389 STUN used by the iOS client
// when UDP STUN is unreachable (app sandbox, some corporate proxies). The
// caller must authenticate with a signed bearer token so attackers cannot
// anonymously enumerate the server.
func (s *Server) handleSTUN(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		s.writeError(w, http.StatusUnauthorized, "missing_auth", "Authorization: Bearer <token> required")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if _, err := auth.VerifyBearerToken(token); err != nil {
		s.writeError(w, http.StatusUnauthorized, "invalid_token", err.Error())
		return
	}

	// Prefer X-Forwarded-For when the server runs behind a trusted proxy
	// (configured via TRUSTED_PROXY=1). Otherwise use RemoteAddr.
	host, port := remoteIPPort(r)

	s.writeJSON(w, http.StatusOK, map[string]any{
		"ip":   host,
		"port": port,
		"type": "public",
	})
}

// remoteIPPort extracts the caller's source IP and port. When the X-Forwarded-For
// header is present and TRUSTED_PROXY is enabled, the first (leftmost) entry
// is used. Otherwise falls back to the raw RemoteAddr from the TCP connection.
func remoteIPPort(r *http.Request) (string, int) {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); first != "" {
			// No port available from XFF; return 0.
			return first, 0
		}
	}
	host, portStr, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr, 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"devices": s.store.DeviceCount(),
	})
}

// --- Helpers ---

// shortKey returns a prefix of a key for logging, padded/truncated so that short
// or empty inputs never panic on slice bounds. The value is purely informational.
func shortKey(k string) string {
	const n = 8
	if len(k) <= n {
		return k
	}
	return k[:n] + "..."
}

func notificationToPayload(n Notification) *apns.Payload {
	p := &apns.Payload{
		Alert: apns.Alert{
			Title:    n.Title,
			Subtitle: n.Subtitle,
			Body:     n.Body,
		},
		Sound:    "default",
		Category: n.Category,
		ThreadID: n.ThreadID,
	}
	if len(n.Data) > 0 {
		p.Data = make(map[string]any, len(n.Data))
		for k, v := range n.Data {
			p.Data[k] = v
		}
	}
	return p
}

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
