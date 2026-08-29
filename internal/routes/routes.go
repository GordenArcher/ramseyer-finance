package routes

import (
	"net/http"
	authhandlers "ramseyer-finance/internal/handlers/auth"
	backuphandlers "ramseyer-finance/internal/handlers/backup"
	financehandlers "ramseyer-finance/internal/handlers/finance"
	startuphandlers "ramseyer-finance/internal/handlers/startup"
	updatehandlers "ramseyer-finance/internal/handlers/updates"
)

// Register owns the application's complete HTTP surface. Keeping route composition here
// leaves main.go responsible for process bootstrapping only, and gives reviewers one place
// to audit which endpoints are public, authenticated, mutating, or static.
func Register(mux *http.ServeMux, staticHandler http.Handler) {
	// Authentication entry points must remain public so a fresh installation can create its
	// first PIN and a returning operator can unlock the local application.
	mux.HandleFunc("/login", authhandlers.LoginPage)
	mux.HandleFunc("/api/auth/setup", authhandlers.SetupPIN)
	mux.HandleFunc("/api/auth/unlock", authhandlers.Unlock)
	mux.HandleFunc("/api/auth/logout", authhandlers.Logout)
	mux.HandleFunc("/startup", authhandlers.WithAuth(startuphandlers.Page))
	mux.HandleFunc("/api/startup/continue", authhandlers.WithAuth(startuphandlers.Continue))
	mux.HandleFunc("/api/startup/fresh", authhandlers.WithAuth(startuphandlers.FreshStart))

	// Every financial screen and mutation passes through the same PIN session middleware.
	// This explicit table also makes destructive POST endpoints easy to distinguish during
	// security and release reviews.
	mux.HandleFunc("/", authhandlers.WithAuth(financehandlers.Dashboard))
	mux.HandleFunc("/data-entry", authhandlers.WithAuth(financehandlers.DataEntryPage))
	mux.HandleFunc("/transactions", authhandlers.WithAuth(financehandlers.TransactionsPage))
	mux.HandleFunc("/api/transaction/add", authhandlers.WithAuth(financehandlers.AddTransaction))
	mux.HandleFunc("/api/transaction/update", authhandlers.WithAuth(financehandlers.UpdateTransaction))
	mux.HandleFunc("/api/transaction/delete", authhandlers.WithAuth(financehandlers.DeleteTransaction))
	mux.HandleFunc("/api/transactions/export", authhandlers.WithAuth(financehandlers.ExportTransactions))

	mux.HandleFunc("/monthly", authhandlers.WithAuth(financehandlers.MonthlyReport))
	mux.HandleFunc("/quarterly", authhandlers.WithAuth(financehandlers.QuarterlyReport))
	mux.HandleFunc("/annual", authhandlers.WithAuth(financehandlers.AnnualReport))
	mux.HandleFunc("/trial-balance", authhandlers.WithAuth(financehandlers.TrialBalance))
	mux.HandleFunc("/api/report/pdf", authhandlers.WithAuth(financehandlers.ExportReportPDF))
	mux.HandleFunc("/balance-sheet", authhandlers.WithAuth(financehandlers.BalanceSheet))
	mux.HandleFunc("/cash-flow", authhandlers.WithAuth(financehandlers.CashFlowStatement))
	mux.HandleFunc("/fixed-assets", authhandlers.WithAuth(financehandlers.FixedAssetSchedule))
	mux.HandleFunc("/notes", authhandlers.WithAuth(financehandlers.NotesPage))

	mux.HandleFunc("/categories", authhandlers.WithAuth(financehandlers.CategoriesPage))
	mux.HandleFunc("/setup", authhandlers.WithAuth(financehandlers.SetupPage))
	mux.HandleFunc("/api/category/save", authhandlers.WithAuth(financehandlers.SaveCategory))
	mux.HandleFunc("/api/category/toggle", authhandlers.WithAuth(financehandlers.ToggleCategoryStatus))
	mux.HandleFunc("/api/budget/save", authhandlers.WithAuth(financehandlers.SaveBudget))
	mux.HandleFunc("/api/budget/delete", authhandlers.WithAuth(financehandlers.DeleteBudget))
	mux.HandleFunc("/api/settings/dashboard-greeting/save", authhandlers.WithAuth(financehandlers.SaveDashboardGreetingSettings))
	mux.HandleFunc("/api/settings/auto-backup/save", authhandlers.WithAuth(backuphandlers.SaveAutoBackupSettings))
	mux.HandleFunc("/api/auth/change-pin", authhandlers.WithAuth(authhandlers.ChangePIN))

	mux.HandleFunc("/backup", authhandlers.WithAuth(backuphandlers.BackupPage))
	mux.HandleFunc("/api/backup/download", authhandlers.WithAuth(backuphandlers.DownloadBackup))
	mux.HandleFunc("/api/backup/restore", authhandlers.WithAuth(backuphandlers.RestoreBackup))
	mux.HandleFunc("/api/update/check", authhandlers.WithAuth(updatehandlers.CheckForUpdates))
	mux.HandleFunc("/api/update/apply", authhandlers.WithAuth(updatehandlers.ApplyUpdate))

	mux.Handle("/static/", http.StripPrefix("/static/", staticHandler))
}
