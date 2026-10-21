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
	"os/exec"
	"path/filepath"
	"ramseyer-finance/internal/appmeta"
	"ramseyer-finance/internal/cli"
	"ramseyer-finance/internal/db"
	backupservice "ramseyer-finance/internal/handlers/backup"
	"ramseyer-finance/internal/routes"
	"runtime"
	"strings"
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
	openBrowser, err := commandMode(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	// Resolve the database file path before doing anything else. The database is the
	// application's durable state layer—authentication, transactions, settings, budgets,
	// and audit history all live in SQLite. If the path can't be resolved or the database
	// can't be opened, there's no point in starting the web server.
	dbPath, err := resolveDatabasePath()
	if err != nil {
		log.Fatalf("Failed to resolve database path: %v", err)
	}
	log.Printf("Using database at %s", dbPath)

	// I initialize SQLite before anything else because most of the app contract depends on
	// durable local state: authentication, backups, transactions, audit history, and settings.
	// If storage is not healthy, I prefer failing early instead of opening a half-working server.
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
	stopAutoBackupScheduler := backupservice.StartAutoBackupScheduler()
	defer stopAutoBackupScheduler()
	// Fresh-start recovery files are intentionally temporary. This independent scheduler
	// performs an immediate cleanup after launch and then checks hourly, so a snapshot that
	// expires while the app is closed is removed the next time it starts.
	stopFreshStartCleanup := backupservice.StartFreshStartRecoveryCleanupScheduler()
	defer stopFreshStartCleanup()

	// Create a sub-filesystem rooted at "static/" so that http.FileServerFS serves
	// "/static/styles/style.css" when the browser requests it, without the
	// "static/" prefix appearing in the URL path. fs.Sub strips the leading directory.
	staticSubFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to prepare static assets: %v", err)
	}

	// Route ownership lives outside process bootstrapping so main remains focused on storage,
	// lifecycle, and the HTTP server. The route package receives the embedded static filesystem
	// as a normal handler dependency.
	mux := http.NewServeMux()
	routes.Register(mux, http.FileServerFS(staticSubFS))

	// Bind to a configurable host/port for pure web access
	host := os.Getenv("HOST")
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PORT")
	if strings.TrimSpace(port) == "" {
		port = "47823"
	}
	addr := net.JoinHostPort(host, port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to start server listener: %v", err)
	}
	defer listener.Close()

	server := &http.Server{Handler: mux}
	defer server.Close()

	log.Printf("Server running on http://%s", listener.Addr().String())
	if openBrowser {
		if err := openURL("http://" + listener.Addr().String()); err != nil {
			log.Printf("Could not open browser automatically: %v", err)
		}
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// commandMode handles the installed-binary interface. A normal client invocation controls the
// managed background service; `serve` is deliberately reserved for that service and development.
func commandMode(args []string) (bool, error) {
	if len(args) == 0 || args[0] == "start" {
		if err := cli.Start(); err != nil {
			return false, err
		}
		os.Exit(0)
	}
	switch args[0] {
	case "serve", "--no-browser":
		return false, nil
	case "enable":
		if err := cli.Enable(); err != nil {
			return false, err
		}
		os.Exit(0)
	case "open":
		if err := cli.EnsureRunning(); err != nil {
			return false, err
		}
		if err := openURL(cli.LocalAppURL()); err != nil {
			return false, fmt.Errorf("open browser: %w", err)
		}
		os.Exit(0)
	case "stop":
		if err := cli.Stop(); err != nil {
			return false, err
		}
		os.Exit(0)
	case "status":
		if err := cli.Status(); err != nil {
			return false, err
		}
		os.Exit(0)
	case "health":
		if err := cli.Health(); err != nil {
			return false, err
		}
		os.Exit(0)
	case "update":
		if err := cli.Update(); err != nil {
			return false, err
		}
		os.Exit(0)
	case "version", "--version", "-v":
		fmt.Println(appmeta.CurrentVersion)
		os.Exit(0)
	case "help", "--help", "-h":
		fmt.Println("Usage: ramseyer-finance [start|open|stop|status|health|update|version]\n\n  start    Start the background app without opening a browser (default).\n  open     Start the app if needed, then open it in your default browser.\n  stop     Stop the background app.\n  status   Show whether the app is running.\n  health   Verify the local app is responding.\n  update   Install the latest GitHub Release, then start it.\n  version  Print the installed version.\n  serve    Run in the foreground (used by the background service).")
		os.Exit(0)
	default:
		return false, fmt.Errorf("unknown command %q; run ramseyer-finance help", args[0])
	}
	return false, nil
}

func openURL(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
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
		// This release is intentionally a clean product baseline rather than an in-place
		// database upgrade. A new application-data directory keeps any earlier installation
		// untouched and recoverable while ensuring the installed build starts with the chart,
		// schema, and workflow it was designed for.
		appDir := filepath.Join(configDir, "ramseyer-financial-manager")
		if err := os.MkdirAll(appDir, 0o755); err != nil {
			return "", fmt.Errorf("create app config directory: %w", err)
		}
		return filepath.Join(appDir, "financial-records.db"), nil
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
	return filepath.Join(workingDir, "financial-records.db"), nil
}
