//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteServiceDefinitionCreatesStartupEntry(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)

	if err := writeServiceDefinition(); err != nil {
		t.Fatalf("write startup entry: %v", err)
	}

	path := filepath.Join(
		appData,
		"Microsoft",
		"Windows",
		"Start Menu",
		"Programs",
		"Startup",
		serviceLabel+".cmd",
	)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read startup entry: %v", err)
	}
	if !strings.Contains(string(content), " start") {
		t.Fatalf("startup entry does not call the start command:\n%s", content)
	}
}
