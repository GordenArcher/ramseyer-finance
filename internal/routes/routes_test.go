package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpointUsesTheStandardResponseEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.NotFoundHandler())

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	for _, field := range []string{"status", "http_status", "message", "data", "errors", "code", "request_id"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("health response is missing %q: %#v", field, payload)
		}
	}
	if payload["status"] != "success" || payload["code"] != "SERVICE_HEALTHY" {
		t.Fatalf("unexpected health response: %#v", payload)
	}
}

func TestRegisterOwnsEveryApplicationRoute(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.NotFoundHandler())

	paths := []string{
		"/healthz",
		"/login",
		"/startup",
		"/data-entry",
		"/transactions",
		"/monthly",
		"/quarterly",
		"/annual",
		"/trial-balance",
		"/balance-sheet",
		"/cash-flow",
		"/fixed-assets",
		"/notes",
		"/categories",
		"/setup",
		"/backup",
		"/api/transaction/add",
		"/api/auth/logout",
		"/api/startup/fresh",
		"/api/transactions/export",
		"/api/category/save",
		"/api/settings/auto-backup/save",
		"/api/update/check",
		"/api/backup/restore",
		"/static/scripts/app.js",
	}

	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		_, pattern := mux.Handler(request)
		if pattern == "" || (path != "/" && pattern == "/") {
			t.Fatalf("route %q was not registered explicitly", path)
		}
	}
}

func TestRegisterDoesNotExposeCompetingFinancialWriteRoutes(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.NotFoundHandler())

	for _, path := range []string{
		"/api/trial-balance/save",
		"/api/trial-balance/add",
		"/api/trial-balance/delete",
		"/api/opening-balance/save",
		"/api/fund-rollforward/save",
		"/api/account-opening-balance/save",
		"/api/fixed-asset-opening/save",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		_, pattern := mux.Handler(request)
		if pattern != "/" {
			t.Fatalf("competing financial write route %q is still registered as %q", path, pattern)
		}
	}
}
