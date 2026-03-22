package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

type MessageHandler interface {
	ServeMessage(req []byte) ([]byte, error)
}

type PermissionHandler interface {
	Approve(sessionID string)
	Deny(sessionID string)
}

type Handler struct {
	broker *EventBroker
	msgH   MessageHandler
	permH  PermissionHandler
}

func NewHandler(broker *EventBroker, msgH MessageHandler, permH PermissionHandler) *Handler {
	return &Handler{broker: broker, msgH: msgH, permH: permH}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/acp/message", h.handleMessageRoute)
	mux.HandleFunc("/acp/events", h.handleEventsRoute)
	mux.HandleFunc("/acp/sessions/", h.handleSessionsRoute)
}

func (h *Handler) handleMessageRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.handleMessage(w, r)
}

func (h *Handler) handleEventsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.handleEvents(w, r)
}

func (h *Handler) handleSessionsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/acp/sessions/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	sessionID := parts[0]
	switch parts[1] {
	case "approve":
		h.handleApprove(w, sessionID)
	case "deny":
		h.handleDeny(w, sessionID)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) handleMessage(w http.ResponseWriter, r *http.Request) {
	if h.msgH == nil {
		writeError(w, http.StatusInternalServerError, "message handler not configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	resp, err := h.msgH.ServeMessage(body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	if h.broker == nil {
		writeError(w, http.StatusInternalServerError, "event broker not configured")
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "sessionId required")
		return
	}

	_ = h.broker.ServeSSE(r.Context(), sessionID, w)
}

func (h *Handler) handleApprove(w http.ResponseWriter, sessionID string) {
	if h.permH != nil {
		h.permH.Approve(sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"approved"}`))
}

func (h *Handler) handleDeny(w http.ResponseWriter, sessionID string) {
	if h.permH != nil {
		h.permH.Deny(sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"denied"}`))
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp, _ := json.Marshal(map[string]string{"error": msg})
	_, _ = w.Write(resp)
}

type PipeMessageHandler struct {
	mu       sync.Mutex
	handleFn func(req []byte) ([]byte, error)
}

func NewPipeMessageHandler(fn func(req []byte) ([]byte, error)) *PipeMessageHandler {
	return &PipeMessageHandler{handleFn: fn}
}

func (p *PipeMessageHandler) ServeMessage(req []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.handleFn(req)
}
