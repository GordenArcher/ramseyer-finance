// Package cli contains the small command-line surface for the installed app.
package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"ramseyer-finance/internal/appmeta"
	"runtime"
	"strings"
	"time"
)

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

// Update installs the matching asset from the latest public GitHub release, then starts it.
// Releases are intentionally selected by exact OS/architecture name instead of by a URL guessed
// from a tag, so a missing or wrongly named release fails safely.
func Update() error {
	rel, err := latestRelease()
	if err != nil {
		return err
	}
	if compareVersions(appmeta.CurrentVersion, rel.TagName) >= 0 {
		fmt.Printf("Ramseyer Finance %s is already up to date.\n", appmeta.CurrentVersion)
		return nil
	}

	wanted := assetName()
	var download asset
	for _, candidate := range rel.Assets {
		if candidate.Name == wanted {
			download = candidate
			break
		}
	}
	if download.URL == "" {
		return fmt.Errorf("release %s does not contain %s", rel.TagName, wanted)
	}

	stage, err := os.MkdirTemp("", "ramseyer-finance-update-*")
	if err != nil {
		return fmt.Errorf("create update staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	archivePath := filepath.Join(stage, download.Name)
	if err := fetch(download.URL, archivePath); err != nil {
		return err
	}
	if err := verifyDigest(download.Digest, archivePath); err != nil {
		return err
	}
	newBinary := filepath.Join(stage, executableName())
	if err := extractBinary(archivePath, newBinary); err != nil {
		return err
	}
	if err := os.Chmod(newBinary, 0o755); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("mark updated binary executable: %w", err)
	}
	wasRunning := healthy()
	if wasRunning {
		// Stop before replacement so the next managed start always runs the new binary rather
		// than finding an older healthy server still bound to the local port.
		if err := Stop(); err != nil {
			return fmt.Errorf("stop running app before update: %w", err)
		}
	}

	current, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate installed binary: %w", err)
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		return fmt.Errorf("resolve installed binary: %w", err)
	}
	if runtime.GOOS == "windows" {
		return replaceOnWindows(current, newBinary)
	}
	if err := os.Rename(newBinary, current); err != nil {
		return fmt.Errorf("replace installed binary at %s: %w", current, err)
	}
	fmt.Printf("Updated to %s. Starting Ramseyer Finance...\n", rel.TagName)
	return exec.Command(current, "start").Start()
}

func latestRelease() (release, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", appmeta.GitHubOwner, appmeta.GitHubRepo)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "ramseyer-finance")
	response, err := client.Do(request)
	if err != nil {
		return release{}, fmt.Errorf("check GitHub Releases: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("check GitHub Releases: received HTTP %d", response.StatusCode)
	}
	var result release
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return release{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if strings.TrimSpace(result.TagName) == "" {
		return release{}, fmt.Errorf("latest GitHub release has no version tag")
	}
	return result, nil
}

func assetName() string {
	extension := "tar.gz"
	if runtime.GOOS == "windows" {
		extension = "zip"
	}
	return fmt.Sprintf("ramseyer-finance-%s-%s.%s", runtime.GOOS, runtime.GOARCH, extension)
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "ramseyer-finance.exe"
	}
	return "ramseyer-finance"
}

func fetch(url, target string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	request.Header.Set("User-Agent", "ramseyer-finance")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download update: received HTTP %d", response.StatusCode)
	}
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create update archive: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return fmt.Errorf("save update archive: %w", err)
	}
	return file.Sync()
}

func verifyDigest(digest, filePath string) error {
	if strings.TrimSpace(digest) == "" {
		return nil
	} // Older GitHub releases do not expose one.
	algorithm, expected, ok := strings.Cut(strings.TrimSpace(digest), ":")
	if !ok || !strings.EqualFold(algorithm, "sha256") {
		return fmt.Errorf("unsupported release checksum format")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected) {
		return fmt.Errorf("downloaded update checksum does not match GitHub Release")
	}
	return nil
}

func extractBinary(archivePath, target string) error {
	if runtime.GOOS == "windows" {
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return fmt.Errorf("open update ZIP: %w", err)
		}
		defer reader.Close()
		for _, file := range reader.File {
			if filepath.Base(file.Name) != executableName() || file.FileInfo().IsDir() {
				continue
			}
			return copyArchiveFile(file.Open, target)
		}
	} else {
		file, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer file.Close()
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("open update tarball: %w", err)
		}
		defer gzipReader.Close()
		reader := tar.NewReader(gzipReader)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("read update tarball: %w", err)
			}
			if filepath.Base(header.Name) == executableName() && header.Typeflag == tar.TypeReg {
				return copyToFile(reader, target)
			}
		}
	}
	return fmt.Errorf("update archive does not contain %s", executableName())
}

func copyArchiveFile(open func() (io.ReadCloser, error), target string) error {
	reader, err := open()
	if err != nil {
		return err
	}
	defer reader.Close()
	return copyToFile(reader, target)
}

func copyToFile(source io.Reader, target string) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(file, source); err != nil {
		return err
	}
	return file.Sync()
}

func replaceOnWindows(current, replacement string) error {
	// A running Windows executable cannot overwrite itself. cmd starts after this process exits,
	// replaces the file, and launches the normal command without leaving a terminal window open.
	command := fmt.Sprintf(`ping 127.0.0.1 -n 2 > nul & move /Y "%s" "%s" > nul & start "" "%s"`, replacement, current, current)
	if err := exec.Command("cmd", "/C", command).Start(); err != nil {
		return fmt.Errorf("schedule Windows replacement: %w", err)
	}
	fmt.Println("Update downloaded. Ramseyer Finance will restart in a moment.")
	return nil
}

func compareVersions(left, right string) int {
	parse := func(value string) [3]int {
		var result [3]int
		fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(value), "v"), "%d.%d.%d", &result[0], &result[1], &result[2])
		return result
	}
	a, b := parse(left), parse(right)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
