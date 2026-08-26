package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterOwnsEveryApplicationRoute(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, http.NotFoundHandler())

	paths := []string{
		"/login",
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
