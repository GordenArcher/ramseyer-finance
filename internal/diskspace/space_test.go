package diskspace

import (
	"math"
	"strings"
	"testing"
)

func TestEnsureAvailableReportsInsufficientStorage(t *testing.T) {
	directory := t.TempDir()
	if err := EnsureAvailable(directory, 1); err != nil {
		t.Fatalf("one-byte storage check failed: %v", err)
	}

	err := EnsureAvailable(directory, math.MaxUint64)
	if err == nil || !strings.Contains(err.Error(), "not enough free storage") {
		t.Fatalf("maximum storage check error = %v, want insufficient-storage message", err)
	}
}
