package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/acp-remote/pkg/auth"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthValid(t *testing.T) {
	handler := auth.Middleware("secret")(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthInvalid(t *testing.T) {
	handler := auth.Middleware("secret")(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body != `{"error":"unauthorized"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestAuthMissing(t *testing.T) {
	handler := auth.Middleware("secret")(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthEmptyTokenSkips(t *testing.T) {
	// Empty token = dev mode, no auth required
	handler := auth.Middleware("")(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (no auth), got %d", rec.Code)
	}
}

func TestAuthWrongScheme(t *testing.T) {
	handler := auth.Middleware("secret")(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
