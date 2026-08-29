package startupstate

import (
	"path/filepath"
	"ramseyer-finance/internal/db"
	"testing"
)

func TestMissingStateBelongsToExistingInstallation(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "state.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()

	required, err := DecisionRequired()
	if err != nil {
		t.Fatalf("load missing decision state: %v", err)
	}
	if required {
		t.Fatalf("missing decision state interrupted an existing installation")
	}
}

func TestPendingStateBecomesComplete(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "state.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()

	if err := MarkPending(); err != nil {
		t.Fatalf("mark pending: %v", err)
	}
	required, err := DecisionRequired()
	if err != nil || !required {
		t.Fatalf("pending decision required = %v, err = %v", required, err)
	}
	if err := MarkComplete(); err != nil {
		t.Fatalf("mark complete: %v", err)
	}
	required, err = DecisionRequired()
	if err != nil || required {
		t.Fatalf("completed decision required = %v, err = %v", required, err)
	}
}
