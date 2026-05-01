package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/handlers"
	"ramseyer-finance/internal/nativepicker"
	"strings"

	webview "github.com/webview/webview_go"
)

// staticFS embeds the entire static/ directory (CSS, JavaScript, images, fonts) into the
// compiled binary. This means the application is a single self-contained executable with
// no external file dependencies—no need to ship a "static" folder alongside the binary
// or worry about incorrect relative paths. The embedded filesystem is accessed via the
// standard io/fs interface, making it compatible with http.FileServerFS for serving
// static assets over HTTP.
//
//go:embed static/*
var staticFS embed.FS

func main() {
	// Resolve the database file path before doing anything else. The database is the
	// application's durable state layer—authentication, transactions, settings, budgets,
	// and audit history all live in SQLite. If the path can't be resolved or the database
	// can't be opened, there's no point in starting the web server or the desktop shell.
	dbPath, err := resolveDatabasePath()
	if err != nil {
		log.Fatalf("Failed to resolve database path: %v", err)
	}
	log.Printf("Using database at %s", dbPath)

	// I initialize SQLite before anything else because most of the app contract depends on
	// durable local state: authentication, backups, transactions, audit history, and settings.
	// If storage is not healthy, I prefer failing early instead of opening a half-working shell.
	// Initialize opens (or creates) the database, applies the schema, runs migrations,
	// seeds the category chart of accounts, and backfills any denormalised data. If any
	// of these steps fail, the application logs the error and exits immediately rather
	// than running with a partially-initialised database.
	if err := db.Initialize(dbPath); err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()

	// I start the auto-backup scheduler once at process boot so the same policy applies
	// whether the user leaves the app open all day or reopens it after missing a backup window.
	// The scheduler itself still does due checks instead of blindly writing on each tick.
	// The scheduler runs in a background goroutine and performs an immediate due-check on
	// startup (to catch backups that should have run while the app was closed) and then
	// checks every 12 hours. The returned stop function is deferred so the scheduler
	// goroutine is cleanly shut down when main() exits.
	stopAutoBackupScheduler := handlers.StartAutoBackupScheduler()
	defer stopAutoBackupScheduler()

	// Create a sub-filesystem rooted at "static/" so that http.FileServerFS serves
	// "/static/style.css" when the browser requests "/static/style.css", without the
	// "static/" prefix appearing in the URL path. fs.Sub strips the leading directory.
	staticSubFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to prepare static assets: %v", err)
	}

	// I keep the HTTP surface local and explicit because the desktop shell is just a wrapper
	// around this server. Having every route listed here makes it easier to reason about
	// authenticated areas, destructive endpoints, and future packaging decisions.
	// Every route is registered on a single ServeMux. Public routes (login, auth API) are
	// registered without middleware. All other routes are wrapped with handlers.WithAuth,
	// which redirects unauthenticated requests to the login page. API routes for
	// destructive actions (create, update, delete, restore) use POST-only handlers to
	// prevent accidental invocation via GET requests or link prefetching.
	mux := http.NewServeMux()
	mux.HandleFunc("/login", handlers.LoginPage)
	mux.HandleFunc("/api/auth/setup", handlers.SetupPIN)
	mux.HandleFunc("/api/auth/unlock", handlers.Unlock)
	mux.HandleFunc("/api/auth/logout", handlers.Logout)
	mux.HandleFunc("/", handlers.WithAuth(handlers.Dashboard))
	mux.HandleFunc("/data-entry", handlers.WithAuth(handlers.DataEntryPage))
	mux.HandleFunc("/transactions", handlers.WithAuth(handlers.TransactionsPage))
	mux.HandleFunc("/api/transaction/add", handlers.WithAuth(handlers.AddTransaction))
	mux.HandleFunc("/api/transaction/update", handlers.WithAuth(handlers.UpdateTransaction))
	mux.HandleFunc("/api/transaction/delete", handlers.WithAuth(handlers.DeleteTransaction))
	mux.HandleFunc("/api/transactions/export", handlers.WithAuth(handlers.ExportTransactions))
	mux.HandleFunc("/monthly", handlers.WithAuth(handlers.MonthlyReport))
	mux.HandleFunc("/quarterly", handlers.WithAuth(handlers.QuarterlyReport))
	mux.HandleFunc("/annual", handlers.WithAuth(handlers.AnnualReport))
	mux.HandleFunc("/balance-sheet", handlers.WithAuth(handlers.BalanceSheet))
	mux.HandleFunc("/notes", handlers.WithAuth(handlers.NotesPage))
	mux.HandleFunc("/setup", handlers.WithAuth(handlers.SetupPage))
	mux.HandleFunc("/api/budget/save", handlers.WithAuth(handlers.SaveBudget))
	mux.HandleFunc("/api/budget/delete", handlers.WithAuth(handlers.DeleteBudget))
	mux.HandleFunc("/api/opening-balance/save", handlers.WithAuth(handlers.SaveOpeningBalance))
	mux.HandleFunc("/api/settings/auto-backup/save", handlers.WithAuth(handlers.SaveAutoBackupSettings))
	mux.HandleFunc("/api/auth/change-pin", handlers.WithAuth(handlers.ChangePIN))
	mux.HandleFunc("/backup", handlers.WithAuth(handlers.BackupPage))
	mux.HandleFunc("/api/backup/download", handlers.WithAuth(handlers.DownloadBackup))
	mux.HandleFunc("/api/backup/restore", handlers.WithAuth(handlers.RestoreBackup))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(staticSubFS)))

	// Bind to an ephemeral port on localhost only. Listening on 127.0.0.1 ensures the
	// server is not accessible from other machines on the network. Port 0 tells the
	// operating system to assign any available port, which prevents conflicts with other
	// applications and allows multiple instances of the app to run simultaneously (each
	// gets its own port). The actual assigned address is passed to the webview for
	// navigation.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to start local listener: %v", err)
	}
	defer listener.Close()

	server := &http.Server{Handler: mux}
	defer server.Close()

	// I bind to an ephemeral localhost port so multiple dev runs do not collide on a fixed port
	// and so packaged builds do not need the user to manage server startup manually.
	// Start the HTTP server in a background goroutine. The server runs for the lifetime
	// of the application and is shut down when main() returns (via the deferred Close).
	// The listener's address is logged so developers can see which port was assigned.
	go func() {
		fmt.Printf("Server running on http://%s\n", listener.Addr().String())
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	// I use a webview shell instead of building a separate native UI because the product is
	// still HTML-driven, but I still bind native helpers like the restore picker where embedded
	// browser behavior is weaker than a real desktop app.
	// Create a new webview window (false = no debug inspector). The webview wraps the
	// HTML/CSS/JS frontend in a native desktop window without requiring Electron or a
	// separate browser. The window title and default size are set before navigation.
	// The native backup file picker (Go function) is bound to a JavaScript global
	// (window.pickBackupFile) so the frontend can invoke the OS-native file dialog
	// from the restore form, which works around limitations in embedded webview file
	// input handling.
	w := webview.New(false)
	defer w.Destroy()
	if err := w.Bind("pickBackupFile", nativepicker.PickBackupFile); err != nil {
		log.Fatalf("Failed to bind native backup picker: %v", err)
	}
	w.SetTitle("Ramseyer Presbyterian Church — Financial Manager")
	w.SetSize(1280, 800, webview.HintNone)
	w.Navigate("http://" + listener.Addr().String())
	// Run enters the webview's main event loop. This call blocks until the window is
	// closed by the user. When w.Run() returns, the deferred functions execute in
	// reverse order: w.Destroy() cleans up the webview, server.Close() shuts down the
	// HTTP server, listener.Close() releases the port, stopAutoBackupScheduler()
	// stops the background goroutine, and db.Close() closes the database connection.
	w.Run()
}

// resolveDatabasePath determines where the SQLite database file should be stored.
// It follows a three-tier resolution strategy:
//  1. RAMSEYER_FINANCE_DB_PATH environment variable — for deployments that want
//     the database in a specific managed location (e.g., a network drive or a
//     cloud-synced folder).
//  2. User config directory — the standard OS-specific location for application
//     data (e.g., ~/.config/ramseyer-finance/ on Linux, ~/Library/Application
//     Support/ramseyer-finance/ on macOS, %AppData%/ramseyer-finance/ on Windows).
//     This is the preferred default because it survives across application restarts
//     and works correctly when running via `go run` (which builds into a temp dir).
//  3. Current working directory — a last-resort fallback for environments where
//     the user config directory cannot be resolved.
//
// The parent directory is created if it doesn't exist, with 0755 permissions.
func resolveDatabasePath() (string, error) {
	// I leave an explicit override for deployments that want the database in a managed location,
	// but I do not require it because local desktop use should work without environment setup.
	if customPath := strings.TrimSpace(os.Getenv("RAMSEYER_FINANCE_DB_PATH")); customPath != "" {
		if err := os.MkdirAll(filepath.Dir(customPath), 0o755); err != nil {
			return "", fmt.Errorf("create custom database directory: %w", err)
		}
		return customPath, nil
	}

	// I prefer the user config directory over the executable directory because `go run main.go`
	// builds into a temporary path. Using the config location keeps one stable database file
	// across restarts in development and in normal desktop use.
	// os.UserConfigDir returns the OS-specific configuration directory. On Linux this is
	// typically ~/.config, on macOS ~/Library/Application Support, and on Windows
	// %AppData%. The application creates its own subdirectory within this location.
	configDir, err := os.UserConfigDir()
	if err == nil && strings.TrimSpace(configDir) != "" {
		appDir := filepath.Join(configDir, "ramseyer-finance")
		if err := os.MkdirAll(appDir, 0o755); err != nil {
			return "", fmt.Errorf("create app config directory: %w", err)
		}
		return filepath.Join(appDir, "ramseyer-finance.db"), nil
	}

	// I keep the working directory fallback only as a last resort so the app still starts even
	// in unusual environments where the config directory cannot be resolved.
	// This fallback is rarely reached on modern desktop operating systems but provides
	// a safety net for minimal containers, sandboxed environments, or systems where the
	// home directory is not configured.
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory fallback: %w", err)
	}
	return filepath.Join(workingDir, "ramseyer-finance.db"), nil
}
