// Package startupstate persists whether a newly installed application still needs the
// one-time financial-record choice. It is separate from the HTTP handlers so authentication,
// startup, and restore flows can share the rule without importing one another or creating a
// package cycle.
package startupstate

import "ramseyer-finance/internal/db"

const (
	decisionSettingKey = "startup.financial_record_choice"
	pendingValue       = "pending"
	completeValue      = "complete"
)

// MarkPending is called only when the application creates its first PIN. That moment is a
// reliable fresh-install boundary: older databases may not contain this setting, and treating
// a missing value as pending would incorrectly introduce the prompt during an ordinary upgrade.
func MarkPending() error {
	return db.SetSetting(decisionSettingKey, pendingValue)
}

// MarkComplete records either valid outcome—continuing the existing record or beginning an
// empty one. The selected outcome does not need to be stored because the financial database
// itself already reflects it; only the fact that the one-time decision was made affects login.
func MarkComplete() error {
	return db.SetSetting(decisionSettingKey, completeValue)
}

// DecisionRequired returns true only for an explicit pending marker. A missing value belongs
// to an installation created before this lifecycle marker existed, so it is deliberately
// treated as complete instead of interrupting an established operator on their next login.
func DecisionRequired() (bool, error) {
	value, err := db.GetSetting(decisionSettingKey)
	if err != nil {
		return false, err
	}
	return value == pendingValue, nil
}
