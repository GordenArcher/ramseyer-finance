package handlers

import (
	"net/http"
	"strings"
)

// SaveAutoBackupSettings handles the POST request from the auto-backup configuration form
// on the setup page. It parses the enabled checkbox state and the frequency selection from
// the form, normalises the frequency through the same function used elsewhere in the backup
// system, persists both values to the database settings table, and redirects back to the
// setup page with a confirmation message. The handler does not start or stop the scheduler
// itself—that responsibility belongs to the application bootstrap, which reads the stored
// settings and manages the background goroutine lifecycle independently.
func SaveAutoBackupSettings(w http.ResponseWriter, r *http.Request) {
	// I keep this as a dedicated handler even though the write itself is small because backup policy
	// is operational state with its own validation and redirect path from the setup screen.
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	// The enabled checkbox emits no value when unchecked, so we detect the enabled state by
	// checking whether the form field is present and non-empty. An unchecked box results in
	// an empty string, which is evaluated as false and disables automatic backups.
	enabled := strings.TrimSpace(r.FormValue("enabled")) != ""
	// Normalise the frequency before persisting so the stored value is always a recognised
	// string ("weekly", "monthly", or "quarterly"), regardless of what the form submitted.
	frequency := normalizeAutoBackupFrequency(r.FormValue("frequency"))
	if err := saveAutoBackupConfig(enabled, frequency); err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=Auto+backup+settings+saved", http.StatusSeeOther)
}
