# Ramseyer Finance

Ramseyer Finance is a web-based finance manager for church operations. It runs a Go HTTP server, stores data in SQLite, supports PIN-based local access control, keeps backups locally, and can export register data to CSV, Excel-compatible XML, and PDF. Access it via your web browser.

## Tech Stack

- Go backend with `net/http` and SQLite (`modernc.org/sqlite`)
- Embedded HTML/CSS/JS frontend with canvas charts

## What It Does

- Tracks `income`, `expenditure`, `asset`, and `liability` transactions
- Shows dashboard summaries plus monthly, quarterly, financial-performance, accumulated-fund, balance-sheet, trial-balance, cash-flow, notes, and fixed-asset views
- Uses the updated PCG standard chart of accounts and Notes 3–28 from the congregation workbook
- Carries account openings, fixed-asset openings, depreciation, and accumulated-fund roll-forwards into year-end reports
- Supports transaction editing, deletion, pagination, and audit history
- Supports manual backup, restore, scheduled auto-backup, and export
- Replaces visible native dropdowns with reusable in-app searchable selectors
- Can be installed as a `ramseyer-finance` command and updated from GitHub Releases
- Uses a local PIN screen for first-run setup and later unlock

## Project Layout

- `main.go`: application bootstrap, embedded assets, local server, and command routing
- `internal/cli/`: release updates and per-user background service management
- `internal/db/`: SQLite schema, migrations, and backup/restore helpers
- `internal/appmeta/`: embedded version and release-channel metadata
- `internal/routes/`: the complete HTTP route table and middleware composition
- `internal/handlers/finance/`: clearly named statement, transaction, dashboard, category, and settings handlers
- `internal/handlers/auth/`: PIN authentication and route middleware
- `internal/handlers/backup/`: backup routes, scheduling, history, file storage, and restore-library services
- `internal/handlers/updates/`: GitHub Release update checks for the web interface
- `internal/handlers/viewmodels/`: shared presentation models used across report handlers
- `internal/webui/`: shared rendering with templates grouped into `auth/`, `pages/`, `reports/`, and `partials/`
- `scripts/`: client installation, update, version, and asset maintenance scripts
- `static/scripts/`: shared page behavior, charts, and reusable JavaScript components
- `static/styles/`: base styling and reusable component styles
- `docs/`: installation, operations, and distribution notes

## Local Development

### Requirements

- Go `1.25+`
- A web browser for using the local interface

### Run

```bash
go run main.go serve
```

The app logs the active SQLite path on startup. By default it uses the user config directory, so rerunning `go run main.go` continues using the same database.

### Test

```bash
go test ./...
```

### Build

```bash
go build -o build/ramseyer-finance main.go
```

## Data Locations

### Default database path

- Windows: `%AppData%\ramseyer-financial-manager\financial-records.db`
- macOS: `~/Library/Application Support/ramseyer-financial-manager/financial-records.db`
- Linux: `~/.config/ramseyer-financial-manager/financial-records.db`
- Fallback: `./financial-records.db` if the user config directory is unavailable

### Override database path

```bash
RAMSEYER_FINANCE_DB_PATH=/absolute/path/ramseyer-finance.db go run main.go serve
```

### Default backup and export paths

Manual downloads and native exports go to the user's Downloads directory.

Automatic backups and pre-restore safety backups use subdirectories under the backup root:

- Windows: `%USERPROFILE%\Downloads\Ramseyer Finance Backups\Auto` and `...\Safety`
- macOS/Linux: `~/Downloads/Ramseyer Finance Backups/Auto` and `.../Safety`

### Override backup root

```bash
RAMSEYER_FINANCE_BACKUP_DIR=/absolute/path/to/backups go run main.go serve
```

## Packaging

### Client command-line install and updates

Once a version tag has been published as a GitHub Release, the client needs to copy and run
only one command for their computer. The installer detects its operating system/CPU, downloads
the right binary, adds it to PATH, and opens the app in the browser. No Go, source checkout,
or ZIP extraction is needed.

**macOS / Linux:**

```bash
curl -fsSL "https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.sh?cb=$(date +%s)" | bash
```

**Windows PowerShell:**

```powershell
irm "https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.ps1?cb=$(Get-Random)" | iex
```

The `cb` query parameter is a cache buster. Raw GitHub URLs are cached at the CDN for five
minutes, so without it a client can execute a stale installer that predates a fix.

Then use:

```text
ramseyer-finance           # starts the background app silently (default)
ramseyer-finance start     # starts it if it was stopped, without opening a browser
ramseyer-finance open      # opens it in the browser; starts it only if needed
ramseyer-finance stop      # stops the background app
ramseyer-finance status    # reports whether it is running
ramseyer-finance health    # verifies the local server is responding
ramseyer-finance update    # downloads the latest matching GitHub Release and restarts it
ramseyer-finance update -f # reinstalls the current version to repair a broken install
ramseyer-finance version   # prints the installed version
ramseyer-finance serve     # starts without opening a browser
```

The installers verify the downloadable archive against the GitHub Release checksum, stop any
running instance before replacing the binary, and confirm the app answers `/healthz` before
printing success. When an install is broken but no newer release exists, run
`ramseyer-finance update --force` or run the installer again.

The app listens only on `127.0.0.1:47823` by default. It is local to the client machine;
the browser is its user interface. Installation creates a per-user background service that
starts silently when the client signs in. On Windows, the installer adds a **Ramseyer Finance**
Desktop shortcut that opens the browser interface without showing a terminal window. If the
server fails to start silently, check `%APPDATA%\ramseyer-financial-manager\logs\server.log`
(Windows) or `~/.config/ramseyer-financial-manager/logs/server.log` (Linux/macOS).

### Publishing the CLI releases

Pushing a semantic-version tag, such as `v1.2.3`, runs `.github/workflows/release.yml`.
It creates a GitHub Release with these client assets:

- `ramseyer-finance-darwin-amd64.tar.gz`
- `ramseyer-finance-darwin-arm64.tar.gz`
- `ramseyer-finance-linux-amd64.tar.gz`
- `ramseyer-finance-linux-arm64.tar.gz`
- `ramseyer-finance-windows-amd64.zip`

Before pushing a release tag, set `internal/appmeta/meta.go`'s `CurrentVersion` to the same
version. For example, set it to `v1.2.3`, then run:

```bash
git tag v1.2.3
git push origin v1.2.3
```

The workflow creates the GitHub Release and uploads all matching platform assets. The client
installer and `ramseyer-finance update` select assets by their exact names, so do not rename them.

## Operational Notes

- The first launch asks the operator to create a PIN.
- The app uses SQLite on disk, not in-memory storage.
- Automatic backups are configured from the Setup screen.
- Restore creates a safety backup before replacing the current database.
- Register exports are saved locally so the embedded desktop shell does not replace the screen with raw file output.

## Documentation

- [Installation Guide](docs/INSTALLATION.md)
- [Distribution Guide](docs/DISTRIBUTION.md)
- [Operations Guide](docs/OPERATIONS.md)
- [Windows Deployment Notes](docs/WINDOWS.md)
- [GitHub Release Checklist](docs/GITHUB_RELEASES.md)

## License

Proprietary — built for Ramseyer Presbyterian Church.
