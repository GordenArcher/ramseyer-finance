package backup

import (
	"log"
	"net/http"
	"strings"
)

func serverError(w http.ResponseWriter, err error) {
	log.Printf("backup request failed: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func badRequest(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusBadRequest)
}

func queryEscape(raw string) string {
	replacer := strings.NewReplacer(" ", "+", "\"", "", "#", "", "&", "and", "?", "")
	return replacer.Replace(raw)
}

func alertTone(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return "success"
	}
	for _, marker := range []string{
		"invalid", "must", "replace", "failed", "required", "no backup file",
	} {
		if strings.Contains(normalized, marker) {
			return "warning"
		}
	}
	return "success"
}
