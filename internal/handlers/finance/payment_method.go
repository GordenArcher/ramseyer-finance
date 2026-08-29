package finance

import (
	"fmt"
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
