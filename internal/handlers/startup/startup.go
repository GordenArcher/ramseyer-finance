// Package startup owns the post-login decision between resuming the existing ledger and
// beginning a fresh financial record. Keeping this flow separate from authentication makes
// it clear that a fresh start changes accounting data, not the PIN or application version.
package startup

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	backuphandlers "ramseyer-finance/internal/handlers/backup"
	"ramseyer-finance/internal/startupstate"
	"ramseyer-finance/internal/webui"
	"strings"
	"time"
)

// PageData describes the three states rendered by the startup screen: the initial choice,
// the backup decision, and the completion receipt that carries a direct restore action.
type PageData struct {
	Step               string
	Message            string
	MessageTone        string
	BackupPath         string
	BackupFilename     string
	BackupExpiresLabel string
}

// Page renders the startup choice after a successful unlock. Recovery paths supplied in
// the completion redirect are revalidated against the managed backup library before they
// are displayed or submitted for restore, so a manipulated query string cannot turn this
// screen into an arbitrary local-file reader.
func Page(w http.ResponseWriter, r *http.Request) {
	step := strings.TrimSpace(r.URL.Query().Get("step"))
	if step != "backup" && step != "complete" {
		step = "choice"
	}
	// The completion receipt remains reachable immediately after a successful reset because it
	// may contain the only direct restore link to the three-day safety backup. Every other startup
	// step is protected by the pending marker so typing /startup later cannot reopen a destructive
	// first-install flow that the operator has already resolved.
	if step != "complete" {
		decisionRequired, err := startupstate.DecisionRequired()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		if !decisionRequired {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	data := PageData{
		Step:        step,
		Message:     r.URL.Query().Get("msg"),
		MessageTone: startupMessageTone(r.URL.Query().Get("msg")),
	}
	if step == "complete" {
		requestedPath := strings.TrimSpace(r.URL.Query().Get("backup"))
		if requestedPath != "" {
			validatedPath, err := backuphandlers.ValidateManagedRestorePath(requestedPath)
			if err != nil {
				data.Message = "The recovery backup is no longer available. You can still continue with the fresh financial record."
				data.MessageTone = "warning"
			} else {
				data.BackupPath = validatedPath
				data.BackupFilename = filepath.Base(validatedPath)
				if info, err := os.Stat(validatedPath); err == nil {
					data.BackupExpiresLabel = info.ModTime().Add(backuphandlers.FreshStartRecoveryLifetime).Local().Format("02 Jan 2006 at 15:04")
				}
			}
		}
	}

	webui.RenderStandaloneTemplate(w, "startup", data)
}

// Continue opens the existing database exactly as it was. This is a POST so the initial
// choice has an explicit user action and cannot be triggered by a browser prefetch.
func Continue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := startupstate.MarkComplete(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// FreshStart clears financial records after the operator has explicitly chosen whether to
// create a temporary recovery snapshot. A requested backup must complete successfully before
// the reset begins; low storage or any write failure leaves every existing record untouched.
func FreshStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	backupChoice := strings.ToLower(strings.TrimSpace(r.FormValue("safety_backup")))
	if backupChoice != "yes" && backupChoice != "no" {
		redirectWithMessage(w, r, "Please choose whether to create a safety backup")
		return
	}

	var backupPath string
	if backupChoice == "yes" {
		path, expiresAt, err := backuphandlers.CreateFreshStartRecovery(time.Now())
		if err != nil {
			_ = backuphandlers.RecordBackupEvent("fresh-start-recovery", "failed", "", err.Error())
			redirectWithMessage(w, r, "Fresh start stopped: "+err.Error())
			return
		}
		backupPath = path
		_ = backuphandlers.RecordBackupEvent(
			"fresh-start-recovery",
			"success",
			backupPath,
			"Temporary recovery backup; expires "+expiresAt.Local().Format("02 Jan 2006 15:04"),
		)
	}

	if err := db.ResetFinancialRecords(); err != nil {
		_ = backuphandlers.RecordBackupEvent("fresh-start-reset", "failed", backupPath, err.Error())
		redirectWithMessage(w, r, "The fresh financial record could not be started: "+err.Error())
		return
	}
	if err := startupstate.MarkComplete(); err != nil {
		_ = backuphandlers.RecordBackupEvent("fresh-start-reset", "failed", backupPath, "Financial records cleared but startup completion could not be saved: "+err.Error())
		http.Error(w, "The fresh record was created, but startup completion could not be saved", http.StatusInternalServerError)
		return
	}
	_ = backuphandlers.RecordBackupEvent("fresh-start-reset", "success", backupPath, "Financial records cleared; PIN, settings, and chart of accounts preserved")

	query := url.Values{"step": {"complete"}}
	if backupPath != "" {
		query.Set("backup", backupPath)
	}
	http.Redirect(w, r, "/startup?"+query.Encode(), http.StatusSeeOther)
}

func redirectWithMessage(w http.ResponseWriter, r *http.Request, message string) {
	query := url.Values{
		"step": {"backup"},
		"msg":  {message},
	}
	http.Redirect(w, r, "/startup?"+query.Encode(), http.StatusSeeOther)
}

func startupMessageTone(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	return "warning"
}
