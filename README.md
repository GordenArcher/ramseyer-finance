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

- `main.go`: application bootstrap, embedded assets, local server, desktop webview
- `internal/db/`: SQLite schema, migrations, and backup/restore helpers
- `internal/appmeta/`: embedded version and release-channel metadata
- `internal/routes/`: the complete HTTP route table and middleware composition
- `internal/handlers/finance/`: clearly named statement, transaction, dashboard, category, and settings handlers
- `internal/handlers/auth/`: PIN authentication and route middleware
- `internal/handlers/backup/`: backup routes, scheduling, history, file storage, and restore-library services
- `internal/handlers/updates/`: GitHub Release checks and packaged update staging
- `internal/handlers/viewmodels/`: shared presentation models used across report handlers
- `internal/webui/`: shared rendering with templates grouped into `auth/`, `pages/`, `reports/`, and `partials/`
- `cmd/updater/`: packaged Windows updater helper used for in-app updates
- `cmd/launcher/`: terminal-free Windows GUI entry point
- `internal/desktop/`: minimal desktop bridge for opening trusted external links
- `scripts/`: Windows and macOS build/packaging scripts
- `static/scripts/`: shared page behavior, charts, and reusable JavaScript components
- `static/styles/`: base styling and reusable component styles
- `docs/`: installation, operations, and distribution notes

## Local Development

### Requirements

- Go `1.25+`
- A desktop environment supported by `github.com/webview/webview_go`
- Platform-native webview dependencies available on the build machine

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

- Windows: `%AppData%\ramseyer-finance\ramseyer-finance.db`
- macOS: `~/Library/Application Support/ramseyer-finance/ramseyer-finance.db`
- Linux: `~/.config/ramseyer-finance/ramseyer-finance.db`
- Fallback: `./ramseyer-finance.db` if the user config directory is unavailable

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
curl -fsSL https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.sh | bash
```

**Windows PowerShell:**

```powershell
irm https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.ps1 | iex
```

Then use:

```text
ramseyer-finance           # starts the background app silently (default)
ramseyer-finance start     # starts it if it was stopped, without opening a browser
ramseyer-finance open      # opens it in the browser; starts it only if needed
ramseyer-finance stop      # stops the background app
ramseyer-finance status    # reports whether it is running
ramseyer-finance health    # verifies the local server is responding
ramseyer-finance update    # downloads the latest matching GitHub Release and restarts it
ramseyer-finance version   # prints the installed version
ramseyer-finance serve     # starts without opening a browser
```

The app listens only on `127.0.0.1:47823` by default. It is local to the client machine;
the browser is just its user interface. On future use, the client only runs
`ramseyer-finance`; `ramseyer-finance update` handles updates. Installation creates a
per-user background service that starts when the client signs in to their computer, without
opening a browser. The Windows installer also creates a Desktop shortcut that opens the app.

### Publishing the CLI releases

Pushing a semantic-version tag, such as `v1.2.3`, runs `.github/workflows/release.yml`.
It creates a GitHub Release with these client assets:

- `ramseyer-finance-darwin-amd64.tar.gz`
- `ramseyer-finance-darwin-arm64.tar.gz`
- `ramseyer-finance-linux-amd64.tar.gz`
- `ramseyer-finance-linux-arm64.tar.gz`
- `ramseyer-finance-windows-amd64.zip`

The tag must match `internal/appmeta/meta.go`'s `CurrentVersion`, otherwise the built-in
`update` command will correctly regard the installed version as newer or equal.

### Primary release target: Windows

On Windows PowerShell:

```powershell
.\scripts\build_windows_bundle.ps1
```

This creates:

- `dist\Ramseyer Finance-windows\`
- `dist\Ramseyer Finance-windows.zip`

The extracted package starts by double-clicking `Ramseyer Finance.exe`. The application,
launcher, and updater use the Windows GUI subsystem, so normal start-up and in-app updates
do not open Command Prompt or PowerShell.

The Windows packager script lives in `scripts/build_windows_bundle.ps1`.

### Version bump for the next release

Before tagging a new release, update:

- `internal/appmeta/meta.go` → `CurrentVersion`

Or use:

```bash
./scripts/bump_version.sh v1.2.1
```

### GitHub Release handoff

Recommended client delivery flow:

1. Push a version tag from macOS.
2. GitHub Actions builds the Windows package using a Windows runner.
3. GitHub creates a Release automatically.
4. `dist/Ramseyer Finance-windows.zip` is uploaded as the release asset.
5. Send the client the GitHub Release download link.

That way the client downloads the packaged app directly and does not need to clone the repository or install Go.

### Secondary release target: macOS

```bash
make macos-app
```

This creates:

- `dist/Ramseyer Finance.app`
- `dist/Ramseyer Finance-macos.zip`

The macOS bundle script lives in `scripts/build_macos_app.sh`.

## Operational Notes

- This release starts from a clean application-data location. The previous installation's database is left untouched instead of being upgraded in place.
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
