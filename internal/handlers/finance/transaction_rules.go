package finance

import (
	"fmt"
	"ramseyer-finance/internal/db"
	"strings"
)

// normalizeTransactionDescription trims leading and trailing whitespace from a transaction
// description string. This normalisation is applied before duplicate checking so that two
// otherwise identical transactions are detected as duplicates even if one was submitted
// with extra spaces around the description. It is also applied before the asset/liability
// description requirement check so that a description consisting only of whitespace is
// correctly rejected as empty.
func normalizeTransactionDescription(raw string) string {
	// I normalize descriptions before validation so duplicate checks compare the intended text
	// rather than whitespace differences from form entry.
	return strings.TrimSpace(raw)
}

// transactionAmountValidationMessage explains the different sign rules used by operating
// and balance-sheet postings. Income and expenditure are captured as positive activity,
// while asset and liability accounts must also support decreases such as loan repayments,
// asset disposals, and cash withdrawals. Without signed balance movements those accounts
// could only grow and every later statement would overstate them.
func transactionAmountValidationMessage(transactionType string) string {
	if transactionType == "asset" || transactionType == "liability" {
		return "Amount cannot be zero; use a positive amount for an increase or a negative amount for a decrease"
	}
	return "Amount must be greater than zero"
}

// validateTransactionBusinessRules enforces the application's domain-level validation
// rules for a transaction before it is created or updated. These rules are separate from
// basic form validation (which checks data types, ranges, and required fields) and
// represent business logic that could change over time without affecting the form parsing
// layer. The function accepts a transactionID parameter: when creating a new transaction
// (ID is 0), all rules are applied unconditionally; when updating an existing transaction
// (ID > 0), the duplicate check excludes the transaction itself from consideration so that
// saving an existing row without changes does not trigger a false duplicate warning.
// The allowDuplicate flag provides an escape hatch for intentional repeated entries, which
// the user can enable via a checkbox in the UI after reviewing the duplicate warning.
func validateTransactionBusinessRules(transactionID int64, transactionDate, transactionType string, categoryID int64, description string, amount float64, allowDuplicate bool) error {
	// I reject exact duplicates by default because the most common local data-entry mistake
	// in this app is saving the same posting twice after a slow click or accidental re-open.
	// I still leave an explicit override in the UI for intentional repeats.
	// Duplicate detection is the first rule checked because it is the most common user
	// mistake. The check compares the core business fields: date, type, category,
	// normalised description, and amount. If an exact match is found and the user has
	// not explicitly allowed duplicates, the function returns a descriptive error that
	// the frontend can display alongside a "allow duplicate" checkbox.
	if !allowDuplicate {
		duplicate, err := transactionDuplicateExists(transactionID, transactionDate, transactionType, categoryID, description, amount)
		if err != nil {
			return err
		}
		if duplicate {
			return fmt.Errorf("This looks like a duplicate transaction. Enable the duplicate override if this entry is intentional")
		}
	}

	// Asset and liability transactions require a non-empty description because these
	// entry types represent balance sheet movements (asset acquisitions, disposals,
	// liability incurrences, settlements) where the nature of the transaction is not
	// self-evident from the category alone. Income and expenditure transactions, by
	// contrast, can often be understood from their category, so the description is
	// optional for those types.
	if (transactionType == "asset" || transactionType == "liability") && normalizeTransactionDescription(description) == "" {
		return fmt.Errorf("Description is required for asset and liability entries")
	}

	return nil
}

// transactionDuplicateExists checks whether the database already contains a transaction
// that matches the given core business fields. A duplicate is defined by five attributes:
// the same date, the same transaction type, the same category ID, the same normalised
// description (after trimming whitespace), and the same amount. This definition is
// deliberately strict—it catches the common case of accidentally double-submitting the
// same form, but it does not attempt to detect "fuzzy" duplicates with slightly different
// amounts or descriptions. When transactionID is greater than zero (indicating an update
// to an existing row), the query excludes that row from the count so that saving a
// transaction without changes does not trigger a duplicate warning against itself.
func transactionDuplicateExists(transactionID int64, transactionDate, transactionType string, categoryID int64, description string, amount float64) (bool, error) {
	// I define duplicates by the core business fields that matter for accidental reposting:
	// same date, type, category, normalized description, and amount.
	var count int
	query := `
		SELECT COUNT(*)
		FROM transactions
		WHERE date = ?
			AND type = ?
			AND category_id = ?
			AND COALESCE(description, '') = ?
			AND amount = ?
	`
	args := []interface{}{
		transactionDate,
		transactionType,
		categoryID,
		normalizeTransactionDescription(description),
		amount,
	}
	// When updating an existing transaction, exclude the row being updated from the
	// duplicate check. Without this exclusion, saving a row without changes (or with
	// changes to fields not included in the duplicate definition) would count the row
	// itself as a duplicate and block the update.
	if transactionID > 0 {
		query += ` AND id <> ?`
		args = append(args, transactionID)
	}

	if err := db.DB.QueryRow(query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("check duplicate transaction: %w", err)
	}
	return count > 0, nil
}
