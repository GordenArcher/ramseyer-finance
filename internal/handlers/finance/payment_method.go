package finance

import (
	"database/sql"
	"fmt"
	"ramseyer-finance/internal/db"
	"strings"
)

const defaultPaymentMethod = "cash"

// normalizePaymentMethod keeps the transaction form deliberately lightweight: leaving the
// optional classification untouched records Cash, while submitted values are constrained to
// the four ways the church tracks money movement.
func normalizePaymentMethod(value string) (string, error) {
	method := strings.ToLower(strings.TrimSpace(value))
	if method == "" {
		return defaultPaymentMethod, nil
	}
	switch method {
	case "cash", "momo", "cheque", "bank":
		return method, nil
	default:
		return "", fmt.Errorf("invalid payment method")
	}
}

func paymentMethodLabel(value string) string {
	method, err := normalizePaymentMethod(value)
	if err != nil {
		return "Cash"
	}
	switch method {
	case "momo":
		return "Momo"
	case "cheque":
		return "Cheque"
	case "bank":
		return "Bank"
	default:
		return "Cash"
	}
}

// resolvePaymentAccount converts the visible flag into the internal liquid account used by
// calculated statements. Cheques settle through Bank, but remain visibly labelled Cheque on
// the transaction itself. A matching primary account returns NULL rather than asking the user
// for a second bookkeeping decision.
func resolvePaymentAccount(primaryCategoryID int64, method string) (sql.NullInt64, error) {
	names := []string{"Cash on hand", "Cash"}
	switch method {
	case "momo":
		names = []string{"Momo"}
	case "bank", "cheque":
		names = []string{"Bank"}
	}

	for _, name := range names {
		var categoryID int64
		err := db.DB.QueryRow(`
			SELECT id FROM categories
			WHERE type = 'asset' AND name = ? AND is_active = 1
			ORDER BY CASE WHEN parent_id = 0 THEN 0 ELSE 1 END, id
			LIMIT 1
		`, name).Scan(&categoryID)
		if err == nil {
			if categoryID == primaryCategoryID {
				return sql.NullInt64{}, nil
			}
			return sql.NullInt64{Int64: categoryID, Valid: true}, nil
		}
		if err != sql.ErrNoRows {
			return sql.NullInt64{}, fmt.Errorf("resolve %s payment account: %w", method, err)
		}
	}
	return sql.NullInt64{}, fmt.Errorf("the %s account is not available", paymentMethodLabel(method))
}
