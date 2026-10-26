package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Audit actions match the transaction_audit_log CHECK constraint. Named constants keep
// the register and the importer from inventing a spelling the table will reject.
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
	ActionDeleted = "deleted"
)

// Execer is the part of *sql.DB and *sql.Tx that Record needs. The importer records a
// batch deletion and its replacements inside one transaction, so this cannot be tied to
// the package-level database handle.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// Record appends one immutable snapshot to the audit log. The full JSON payload is stored
// instead of individual columns so history survives changes to the transactions table and
// remains readable after the original row is gone.
func Record(execer Execer, actor, action string, snapshot Snapshot) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal transaction audit snapshot: %w", err)
	}

	if _, err := execer.Exec(`
		INSERT INTO transaction_audit_log (transaction_id, action, actor, snapshot_json)
		VALUES (?, ?, ?, ?)
	`, snapshot.ID, action, actor, string(payload)); err != nil {
		return fmt.Errorf("insert transaction audit log: %w", err)
	}
	return nil
}
