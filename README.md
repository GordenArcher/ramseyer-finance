# Ramseyer Finance

Ramseyer Finance is a local desktop finance manager for church operations. It runs a Go HTTP app inside a desktop webview, stores data in SQLite, supports PIN-based local access control, keeps backups locally, and can export register data to CSV, Excel-compatible XML, and PDF.

## Tech Stack

- Go backend with `net/http` and SQLite (`modernc.org/sqlite`)
- Desktop shell via `github.com/webview/webview_go`
- Embedded HTML/CSS/JS frontend with canvas charts

## What It Does

- Tracks `income`, `expenditure`, `asset`, and `liability` transactions
- Shows dashboard summaries plus monthly, quarterly, annual, balance-sheet, and notes views
- Supports transaction editing, deletion, pagination, and audit history
- Supports manual backup, restore, scheduled auto-backup, and export
- Uses a local PIN screen for first-run setup and later unlock

## Project Layout

- `main.go`: application bootstrap, embedded assets, local server, desktop webview
- `internal/db/`: SQLite schema, migrations, and backup/restore helpers
- `internal/handlers/`: HTTP handlers, reporting logic, auth, exports, backup flows
- `internal/nativepicker/`: desktop restore file picker bridge
- `scripts/`: Windows and macOS build/packaging scripts
- `static/`: JavaScript and CSS used by the embedded UI
- `docs/`: installation, operations, and distribution notes

## Local Development

### Requirements

- Go `1.25+`
- A desktop environment supported by `github.com/webview/webview_go`
- Platform-native webview dependencies available on the build machine

### Run

```bash
go run main.go
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
RAMSEYER_FINANCE_DB_PATH=/absolute/path/ramseyer-finance.db go run main.go
```

### Default backup and export paths

Manual downloads and native exports go to the user's Downloads directory.

Automatic backups and pre-restore safety backups use subdirectories under the backup root:

- Windows: `%USERPROFILE%\Downloads\Ramseyer Finance Backups\Auto` and `...\Safety`
- macOS/Linux: `~/Downloads/Ramseyer Finance Backups/Auto` and `.../Safety`

### Override backup root

```bash
RAMSEYER_FINANCE_BACKUP_DIR=/absolute/path/to/backups go run main.go
```

## Packaging

### Primary release target: Windows

On Windows PowerShell:

```powershell
.\scripts\build_windows_bundle.ps1
```

This creates:

- `dist\Ramseyer Finance-windows\`
- `dist\Ramseyer Finance-windows.zip`

The Windows packager script lives in `scripts/build_windows_bundle.ps1`.

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
