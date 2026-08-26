package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"ramseyer-finance/internal/appmeta"
	backupservice "ramseyer-finance/internal/handlers/backup"
	"runtime"
	"strings"
	"time"
)

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	HTMLURL     string               `json:"html_url"`
	Body        string               `json:"body"`
	PublishedAt string               `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

type updateStatusResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	CanApply        bool   `json:"can_apply"`
	ReleaseURL      string `json:"release_url"`
	PublishedAt     string `json:"published_at"`
	Notes           string `json:"notes"`
	AssetName       string `json:"asset_name"`
	Message         string `json:"message"`
}

type updateApplyResponse struct {
	Message      string `json:"message"`
	QuitRequired bool   `json:"quit_required"`
}

// CheckForUpdates queries the repository's latest GitHub Release and returns structured update
// metadata to the Setup page. I keep this server-side instead of calling GitHub directly from the
// browser so release selection, asset naming, and version comparison all live in one trusted path.
func CheckForUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	release, asset, err := fetchLatestRelease()
	if err != nil {
		updateError(w, fmt.Errorf("check latest GitHub release: %w", err), "Could not check GitHub Releases right now")
		return
	}

	comparison := compareVersions(appmeta.CurrentVersion, release.TagName)
	updateAvailable := comparison < 0

	message := "You already have the latest version installed."
	if updateAvailable {
		message = "A newer version is available."
	}

	backupservice.WriteJSON(w, http.StatusOK, updateStatusResponse{
		CurrentVersion:  appmeta.CurrentVersion,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable,
		CanApply:        updateAvailable && asset.BrowserDownloadURL != "" && runtimeCanApplyInPlaceUpdate() && hasBundledUpdaterHelper(),
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     FormatReleasePublishedAt(release.PublishedAt),
		Notes:           strings.TrimSpace(release.Body),
		AssetName:       asset.Name,
		Message:         message,
	})
}

// ApplyUpdate stages the latest Windows release asset, verifies it when GitHub exposes a digest,
// launches the temp updater helper, and then asks the frontend to terminate the running app.
// The helper does the destructive work only after the main process exits, which is the safest
// way to update a Windows desktop app whose executable cannot overwrite itself while running.
func ApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !runtimeCanApplyInPlaceUpdate() {
		badRequest(w, "In-app updates are only supported in the packaged Windows desktop app")
		return
	}

	release, asset, err := fetchLatestRelease()
	if err != nil {
		updateError(w, fmt.Errorf("load latest release for apply: %w", err), "Could not reach GitHub Releases to prepare the update")
		return
	}
	if compareVersions(appmeta.CurrentVersion, release.TagName) >= 0 {
		backupservice.WriteJSON(w, http.StatusOK, updateApplyResponse{
			Message:      "You already have the latest version installed.",
			QuitRequired: false,
		})
		return
	}

	if err := stageAndLaunchWindowsUpdate(asset); err != nil {
		updateError(w, fmt.Errorf("stage windows update: %w", err), "The update could not be prepared for installation")
		return
	}

	backupservice.WriteJSON(w, http.StatusOK, updateApplyResponse{
		Message:      "Update downloaded. The app will close and restart into the new version.",
		QuitRequired: true,
	})
}

func fetchLatestRelease() (githubRelease, githubReleaseAsset, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	request, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", appmeta.GitHubOwner, appmeta.GitHubRepo),
		nil,
	)
	if err != nil {
		return githubRelease{}, githubReleaseAsset{}, fmt.Errorf("build release check request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "ramseyer-finance-updater")

	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, githubReleaseAsset{}, fmt.Errorf("check latest release: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return githubRelease{}, githubReleaseAsset{}, fmt.Errorf("check latest release: unexpected status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return githubRelease{}, githubReleaseAsset{}, fmt.Errorf("decode latest release: %w", err)
	}

	asset, ok := selectWindowsReleaseAsset(release.Assets)
	if !ok {
		return githubRelease{}, githubReleaseAsset{}, fmt.Errorf("latest release %s does not contain %s", release.TagName, appmeta.WindowsReleaseAssetName)
	}
	return release, asset, nil
}

func selectWindowsReleaseAsset(assets []githubReleaseAsset) (githubReleaseAsset, bool) {
	// I prefer an exact asset name match so the updater binds to the same packaged ZIP we document
	// in the release process, but I still allow a resilient suffix match if the visible name ever
	// changes slightly while keeping the same bundle role.
	for _, asset := range assets {
		if asset.State == "uploaded" && asset.Name == appmeta.WindowsReleaseAssetName {
			return asset, true
		}
	}
	for _, asset := range assets {
		if asset.State == "uploaded" && strings.EqualFold(asset.Name, appmeta.WindowsReleaseAssetName) {
			return asset, true
		}
	}
	for _, asset := range assets {
		if asset.State == "uploaded" && strings.HasSuffix(strings.ToLower(asset.Name), "windows.zip") {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

func runtimeCanApplyInPlaceUpdate() bool {
	return runtime.GOOS == "windows"
}

func hasBundledUpdaterHelper() bool {
	updaterPath, err := bundledUpdaterPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(updaterPath)
	return err == nil
}

func bundledUpdaterPath() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executablePath), "updater.exe"), nil
}

func stageAndLaunchWindowsUpdate(asset githubReleaseAsset) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	appDir := filepath.Dir(executablePath)
	installRoot := filepath.Dir(appDir)
	updaterPath, err := bundledUpdaterPath()
	if err != nil {
		return fmt.Errorf("resolve bundled updater helper: %w", err)
	}
	if _, err := os.Stat(updaterPath); err != nil {
		return fmt.Errorf("bundled updater helper not found at %s", updaterPath)
	}

	stagingDir, err := os.MkdirTemp("", "ramseyer-finance-update-*")
	if err != nil {
		return fmt.Errorf("create update staging directory: %w", err)
	}

	zipPath := filepath.Join(stagingDir, asset.Name)
	if err := downloadReleaseAsset(asset.BrowserDownloadURL, zipPath); err != nil {
		return err
	}
	if err := verifyReleaseDigest(asset.Digest, zipPath); err != nil {
		return err
	}

	tempUpdaterPath := filepath.Join(stagingDir, "updater.exe")
	if err := backupservice.CopyFile(updaterPath, tempUpdaterPath); err != nil {
		return fmt.Errorf("copy updater helper to temp: %w", err)
	}

	relaunchPath := filepath.Join(installRoot, "Ramseyer Finance.exe")
	if _, err := os.Stat(relaunchPath); err != nil {
		relaunchPath = executablePath
	}

	command := exec.Command(
		tempUpdaterPath,
		"--pid", fmt.Sprintf("%d", os.Getpid()),
		"--zip", zipPath,
		"--target", installRoot,
		"--relaunch", relaunchPath,
	)
	command.Dir = stagingDir
	if err := command.Start(); err != nil {
		return fmt.Errorf("launch updater helper: %w", err)
	}
	return nil
}

func downloadReleaseAsset(downloadURL, targetPath string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	request, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("build release asset request: %w", err)
	}
	request.Header.Set("User-Agent", "ramseyer-finance-updater")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download release asset: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("download release asset: unexpected status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("create staged update zip: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, response.Body); err != nil {
		return fmt.Errorf("write staged update zip: %w", err)
	}
	return file.Sync()
}

func verifyReleaseDigest(digest, filePath string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}

	parts := strings.SplitN(digest, ":", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "sha256") {
		return fmt.Errorf("unsupported release asset digest format")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open staged update for digest verification: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash staged update: %w", err)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != strings.ToLower(parts[1]) {
		return fmt.Errorf("downloaded update failed integrity verification")
	}
	return nil
}

func compareVersions(currentVersion, candidateVersion string) int {
	currentParts := parseVersionParts(currentVersion)
	candidateParts := parseVersionParts(candidateVersion)

	maxLength := len(currentParts)
	if len(candidateParts) > maxLength {
		maxLength = len(candidateParts)
	}

	for index := 0; index < maxLength; index++ {
		current := 0
		if index < len(currentParts) {
			current = currentParts[index]
		}
		candidate := 0
		if index < len(candidateParts) {
			candidate = candidateParts[index]
		}
		switch {
		case current < candidate:
			return -1
		case current > candidate:
			return 1
		}
	}
	return 0
}

func parseVersionParts(version string) []int {
	version = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(version), "v"))
	if version == "" {
		return nil
	}

	segments := strings.Split(version, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		number := 0
		for _, character := range segment {
			if character < '0' || character > '9' {
				break
			}
			number = number*10 + int(character-'0')
		}
		parts = append(parts, number)
	}
	return parts
}

// formatReleasePublishedAt converts the GitHub API release timestamp into the human-facing label
// shown in the update modal. I do this server-side so the app keeps one consistent production
// format everywhere instead of leaving date wording to whichever frontend happens to consume it.
func FormatReleasePublishedAt(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	publishedAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}

	day := publishedAt.Day()
	return fmt.Sprintf(
		"%d%s %s, %d at %s",
		day,
		ordinalSuffix(day),
		publishedAt.Format("January"),
		publishedAt.Year(),
		publishedAt.Format("3:04 PM MST"),
	)
}

// ordinalSuffix keeps ordinal-day copy readable for release labels. I special-case 11th, 12th,
// and 13th first because English ordinal rules break the simple last-digit pattern there.
func ordinalSuffix(day int) string {
	if day%100 >= 11 && day%100 <= 13 {
		return "th"
	}

	switch day % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}

func updateError(w http.ResponseWriter, err error, userMessage string) {
	log.Printf("request failed: %v", err)
	http.Error(w, userMessage, http.StatusBadGateway)
}

func badRequest(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusBadRequest)
}
