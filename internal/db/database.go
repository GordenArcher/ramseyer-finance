package db

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// DB is the package-level handle to the SQLite connection. It is guarded by lifecycleMu
// and must only be accessed while holding that lock or during read-only queries that are
// safe against a single-writer WAL connection. A nil value means the database is closed
// or was never initialized.
var DB *sql.DB

// DBPath stores the absolute or relative filesystem path to the active SQLite database file.
// It is set during Initialize and used by backup/restore operations to locate sidecar files
// (WAL and SHM) and to construct sibling temp files on the same filesystem.
var DBPath string

// lifecycleMu serializes all database lifecycle operations: Initialize, Close,
// CreateBackupFile, and RestoreFromReader. By forcing these critical sections to run one
// at a time, we prevent races such as closing the database while a backup is still in
// progress or two goroutines simultaneously trying to restore different payloads.
var lifecycleMu sync.Mutex

// Initialize opens (or replaces) the database connection at the given path. It is designed
// to be called exactly once at application startup, but can safely be called again—for
// instance, after a restore—because it closes any existing connection before opening a new
// one. The function validates the path, stores it globally, and delegates to openLocked
// for the actual connection setup, schema migration, and data seeding.
func Initialize(path string) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	// Close any previously opened database so we don't leak file descriptors or WAL
	// connections when reinitializing (e.g., after a restore operation swaps the file).
	Close()
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("database path is required")
	}
	DBPath = path
	return openLocked()
}

// openLocked performs the actual database opening and initialisation sequence. It must only
// be called while lifecycleMu is held. The function configures WAL journal mode with a 5-second
// busy timeout, limits the connection pool to a single writer (which is required for SQLite in
// WAL mode without a separate connection pool manager), enables foreign key enforcement, and
// then runs the schema creation, migrations, category seeding, and data backfill steps in order.
// If any step fails, it closes the database and returns an error to leave a clean state.
func openLocked() error {
	var err error
	// Append pragma query parameters directly to the DSN so they take effect at connection
	// open time. WAL mode allows concurrent reads while a write is in progress, and the
	// busy timeout makes concurrent writers wait instead of immediately failing with
	// SQLITE_BUSY.
	DB, err = sql.Open("sqlite", DBPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Restrict to one open connection because SQLite in WAL mode combined with Go's
	// database/sql connection pooling can otherwise create multiple write connections,
	// leading to "database is locked" errors under concurrent access.
	DB.SetMaxOpenConns(1)

	// Enable foreign key constraint enforcement. SQLite disables this by default for
	// backwards compatibility, but all table relationships in this schema assume cascading
	// deletes and referential integrity checks are active.
	if _, err := DB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		Close()
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Run the base schema creation. The schema.go file uses IF NOT EXISTS on every
	// statement, so this is safe to call on both fresh and existing databases.
	if _, err := DB.Exec(schema); err != nil {
		Close()
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Each migration step is designed to be idempotent—it checks whether a column or index
	// already exists before attempting to add it. This allows the same code path to handle
	// brand-new databases and databases that have been incrementally upgraded through
	// previous versions of the application.
	if err := migrateTransactions(); err != nil {
		Close()
		return fmt.Errorf("failed to migrate transactions: %w", err)
	}
	if err := migrateCategories(); err != nil {
		Close()
		return fmt.Errorf("failed to migrate categories: %w", err)
	}

	if err := seedCategories(); err != nil {
		Close()
		return fmt.Errorf("failed to seed categories: %w", err)
	}
	if err := seedDashboardGreetings(); err != nil {
		Close()
		return fmt.Errorf("failed to seed dashboard greetings: %w", err)
	}

	if err := backfillTransactionCategoryIDs(); err != nil {
		Close()
		return fmt.Errorf("failed to backfill transaction categories: %w", err)
	}
	if err := syncTransactionCategoryMetadata(); err != nil {
		Close()
		return fmt.Errorf("failed to sync transaction metadata: %w", err)
	}

	return nil
}

// Close shuts down the database connection and sets the DB handle to nil. It is safe to
// call multiple times and on a nil DB (no-op). The function does not remove any files; it
// only releases the in-process connection and allows the operating system to flush any
// pending writes. Callers performing a restore typically call Close before swapping the
// underlying database file.
func Close() {
	if DB != nil {
		_ = DB.Close()
		DB = nil
	}
}

// migrateTransactions applies incremental schema changes to the transactions table. Unlike
// the base schema in schema.go, which describes the ideal final state of a fresh database,
// this function handles the reality of existing databases that were created under older
// versions of the application. Each change is guarded by a column-existence check so the
// migration is safe to run repeatedly. It also creates performance-critical indexes that
// support the dashboard queries (filtering by type/date, grouping by category, etc.).
func migrateTransactions() error {
	// I keep schema changes explicit here because this app already has live SQLite files in the field.
	// Fresh installs get the full schema from schema.go, but existing databases only evolve through these guarded steps.
	exists, err := columnExists("transactions", "category_id")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := DB.Exec("ALTER TABLE transactions ADD COLUMN category_id INTEGER"); err != nil {
			return fmt.Errorf("add category_id column: %w", err)
		}
	}

	updatedAtExists, err := columnExists("transactions", "updated_at")
	if err != nil {
		return err
	}
	if !updatedAtExists {
		// I add the column without an expression default because SQLite does not allow
		// ALTER TABLE ADD COLUMN with non-constant defaults on existing tables.
		// Fresh installs still receive the richer default from the base schema, while
		// migrated databases are backfilled immediately and kept current by the write path.
		if _, err := DB.Exec("ALTER TABLE transactions ADD COLUMN updated_at TEXT"); err != nil {
			return fmt.Errorf("add updated_at column: %w", err)
		}
	}

	// Backfill any rows that still have an empty updated_at value. The COALESCE chain
	// prefers the existing value, then falls back to created_at for migrated rows that
	// already had a creation timestamp, and finally uses the current time as a last resort.
	if _, err := DB.Exec(`
		UPDATE transactions
		SET updated_at = COALESCE(NULLIF(updated_at, ''), created_at, datetime('now','localtime'))
	`); err != nil {
		return fmt.Errorf("backfill updated_at column: %w", err)
	}

	// Index definitions use IF NOT EXISTS so they're safe to run on every startup. The
	// compound index on (type, date) accelerates the primary dashboard view filtered by
	// income or expenditure in date order. The category_id indexes support aggregation by
	// category, and the updated_at index supports any future sync or change-tracking
	// queries.
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_transactions_type_date ON transactions(type, date)",
		"CREATE INDEX IF NOT EXISTS idx_transactions_category_id ON transactions(category_id)",
		"CREATE INDEX IF NOT EXISTS idx_transactions_category_id_date ON transactions(category_id, date)",
		"CREATE INDEX IF NOT EXISTS idx_transactions_updated_at ON transactions(updated_at)",
	}
	for _, stmt := range indexes {
		if _, err := DB.Exec(stmt); err != nil {
			return fmt.Errorf("create transaction index: %w", err)
		}
	}

	return nil
}

// migrateCategories applies incremental schema changes to the categories table. The app now
// treats the categories table as the long-term source of truth, not just a fixed seeded list,
// so these metadata columns are what allow users to manage categories without losing reporting
// structure or archive state.
func migrateCategories() error {
	isActiveExists, err := columnExists("categories", "is_active")
	if err != nil {
		return err
	}
	if !isActiveExists {
		if _, err := DB.Exec("ALTER TABLE categories ADD COLUMN is_active INTEGER NOT NULL DEFAULT 1"); err != nil {
			return fmt.Errorf("add categories.is_active column: %w", err)
		}
	}

	reportSectionExists, err := columnExists("categories", "report_section")
	if err != nil {
		return err
	}
	if !reportSectionExists {
		if _, err := DB.Exec("ALTER TABLE categories ADD COLUMN report_section TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("add categories.report_section column: %w", err)
		}
	}

	if _, err := DB.Exec(`
		UPDATE categories
		SET is_active = 1
		WHERE is_active IS NULL OR is_active NOT IN (0, 1)
	`); err != nil {
		return fmt.Errorf("normalize categories.is_active values: %w", err)
	}

	if _, err := DB.Exec(`
		UPDATE categories
		SET report_section = CASE
			WHEN type = 'asset' AND parent_id = 0 AND name IN ('Property, Plant & Equipment', 'GAP Presbytery', 'Investment (Credit Union)') THEN 'non_current_asset'
			WHEN type = 'asset' AND parent_id = 0 AND name IN ('Receivables (Debtors)', 'Bank', 'Cash', 'Momo') THEN 'current_asset'
			WHEN type = 'liability' AND parent_id = 0 THEN 'current_liability'
			ELSE COALESCE(report_section, '')
		END
		WHERE COALESCE(report_section, '') = ''
	`); err != nil {
		return fmt.Errorf("backfill categories.report_section values: %w", err)
	}

	return nil
}

// columnExists checks whether a given column is present in a table by querying SQLite's
// PRAGMA table_info. This is used by the migration system to determine which schema changes
// have already been applied. It returns true if the column name is found, false if the
// column is absent, and an error if the PRAGMA query itself fails.
func columnExists(tableName, columnName string) (bool, error) {
	rows, err := DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", tableName, err)
	}
	defer rows.Close()

	// PRAGMA table_info returns one row per column with these fields, in order:
	// cid (column order), name, type, notnull flag, default value, primary key flag.
	// We only care about the name field to check for the column's existence.
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey); err != nil {
			return false, fmt.Errorf("scan %s columns: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s columns: %w", tableName, err)
	}

	return false, nil
}

// backfillTransactionCategoryIDs populates the category_id foreign key column for all
// transaction rows where it is still zero or NULL. It joins on the legacy category text
// column and the transaction type (income/expenditure/asset/liability) to find the matching
// category row in the categories table. The LIMIT 1 subquery ensures that if duplicate
// category names exist, only one match is selected, preventing a "subquery returns more
// than one row" error.
func backfillTransactionCategoryIDs() error {
	_, err := DB.Exec(`
		UPDATE transactions
		SET category_id = (
			SELECT c.id
			FROM categories c
			WHERE c.type = transactions.type AND c.name = transactions.category
			LIMIT 1
		)
		WHERE COALESCE(category_id, 0) = 0
	`)
	if err != nil {
		return fmt.Errorf("update transaction category IDs: %w", err)
	}

	// Some early databases stored only the previous workbook's display names. These aliases
	// map those orphaned rows to the closest updated parent account without rewriting the
	// original text until the normal metadata sync runs. This makes the migration recover
	// legacy rows that no longer have an exact category-name match.
	aliases := []struct {
		categoryType string
		legacyName   string
		targetName   string
	}{
		{"income", "Offering", "Offerings"},
		{"income", "Tithe", "Tithes"},
		{"income", "V.T.O.", "Offerings"},
		{"income", "Revivals", "Offerings"},
		{"income", "Other Incomes", "Other Income"},
		{"income", "Group Almanac Days", "Other Income"},
		{"income", "Donation for Manse", "Donations Received"},
		{"expenditure", "Repairs & Maintenance", "Property Upkeep"},
		{"expenditure", "Salaries & Allowances", "Staff Cost"},
		{"expenditure", "Donations", "Social Services"},
		{"expenditure", "Stationery", "General Administration Expenses"},
		{"expenditure", "Assessment", "Contributions Paid"},
		{"expenditure", "Evangelism", "Evangelism Expenses"},
		{"expenditure", "Operating Expenses", "General Administration Expenses"},
		{"expenditure", "Conferences/Training & Retreat", "Training, Seminars, Workshops & Retreats"},
		{"expenditure", "Children Service Expense", "Group & Committee Expenses"},
		{"expenditure", "J.Y. Expense", "Group & Committee Expenses"},
		{"expenditure", "Utilities", "General Administration Expenses"},
		{"expenditure", "Other Expenditure", "General Administration Expenses"},
		{"asset", "Receivables (Debtors)", "Accounts Receivable & Prepayments"},
		{"asset", "GAP Presbytery", "Long Term Investment"},
		{"asset", "Investment (Credit Union)", "Long Term Investment"},
		{"liability", "Payables (Creditors)", "Accounts Payable & Accruals"},
		{"liability", "District Assessment Owing", "Accounts Payable & Accruals"},
	}
	for _, alias := range aliases {
		if _, err := DB.Exec(`
			UPDATE transactions
			SET category_id = (
				SELECT id FROM categories
				WHERE type = ? AND name = ? AND parent_id = 0
				LIMIT 1
			)
			WHERE type = ? AND category = ? AND COALESCE(category_id, 0) = 0
		`, alias.categoryType, alias.targetName, alias.categoryType, alias.legacyName); err != nil {
			return fmt.Errorf("backfill legacy category alias %s: %w", alias.legacyName, err)
		}
	}
	return nil
}

// syncTransactionCategoryMetadata keeps the denormalised category name and note_ref columns
// in the transactions table in sync with the canonical data stored in the categories table.
// This denormalisation allows dashboard queries to retrieve category display data without a
// join, at the cost of needing this periodic sync. The function only updates rows that have a
// valid category_id assigned, and uses COALESCE to preserve existing values if the category
// lookup returns NULL for any reason.
func syncTransactionCategoryMetadata() error {
	_, err := DB.Exec(`
		UPDATE transactions
		SET
			category = COALESCE((
				SELECT c.name
				FROM categories c
				WHERE c.id = transactions.category_id
				LIMIT 1
			), category),
			note_ref = COALESCE((
				SELECT c.note_ref
				FROM categories c
				WHERE c.id = transactions.category_id
				LIMIT 1
			), note_ref)
		WHERE COALESCE(category_id, 0) > 0
	`)
	if err != nil {
		return fmt.Errorf("sync transaction category and note metadata: %w", err)
	}
	return nil
}

// seedCategories populates the categories table with the application's built-in chart of
// accounts. It uses INSERT OR IGNORE so the function is safe to run on every startup—existing
// categories are left untouched. The hierarchy is expressed through the parent column: top-level
// categories have an empty parent string (assigned parent_id 0), while subcategories reference
// their parent by name. Subcategories that cannot find their parent (for example, if the
// parent's INSERT was skipped due to a conflict) are silently skipped. Each category also
// carries a note_ref code that maps to an external accounting reference system.
func seedCategories() error {
	// I ensure the standard chart account-by-account instead of treating it as fresh-install
	// sample data. The updated workbook is now part of the reporting contract, so existing
	// installations need its newly introduced accounts too. INSERT OR IGNORE below preserves
	// archived flags, user renames, and transaction-linked rows while filling only missing
	// standard accounts.
	categories := workbookChartCategories()

	// Iterate through every category definition and insert it into the database. Top-level
	// categories (empty parent field) get parent_id 0. Subcategories look up their parent
	// by name within the same category type; if the parent isn't found, the subcategory is
	// silently skipped to avoid inserting orphaned rows.
	for _, c := range categories {
		var parentID int64 = 0
		if c.Parent != "" {
			// Look up the parent category by name and type, restricted to parent_id=0
			// so we only match top-level categories and never accidentally chain
			// through a subcategory. If the parent doesn't exist yet (e.g., because
			// its INSERT was skipped due to a prior duplicate), we skip this child.
			err := DB.QueryRow(
				"SELECT id FROM categories WHERE type=? AND name=? AND parent_id=0",
				c.CategoryType, c.Parent,
			).Scan(&parentID)
			if err != nil {
				continue
			}
		}
		// INSERT OR IGNORE ensures that running seedCategories on every startup does
		// not produce duplicate rows or errors when categories already exist from a
		// previous run.
		_, err := DB.Exec(
			"INSERT OR IGNORE INTO categories (type, name, parent_id, note_ref, report_section, is_active) VALUES (?, ?, ?, ?, ?, 1)",
			c.CategoryType, c.Name, parentID, c.NoteRef, c.ReportSection,
		)
		if err != nil {
			return fmt.Errorf("failed to insert category %s: %w", c.Name, err)
		}
	}

	// A previous installation may already have Bank, Cash, and Momo as top-level accounts.
	// Fold those durable rows into the workbook's Note 26 parent before updating metadata;
	// this prevents duplicate-looking choices while retaining every existing foreign key.
	if err := migrateLegacyCashHierarchy(); err != nil {
		return fmt.Errorf("migrate legacy cash hierarchy: %w", err)
	}

	// Older releases used a different note vocabulary. I keep those historical categories
	// available, but move their note references onto the updated workbook sections so old
	// transactions continue contributing to the correct statements after an upgrade.
	if err := migrateLegacyCategoryMappings(); err != nil {
		return fmt.Errorf("migrate legacy category mappings: %w", err)
	}
	return nil
}

// migrateLegacyCategoryMappings translates the previous application's top-level account
// names into the updated workbook note structure. The migration intentionally changes only
// report metadata, not transaction values or category IDs, because IDs are the durable links
// used by budgets, audit history, and existing postings.
func migrateLegacyCategoryMappings() error {
	mappings := []struct {
		categoryType string
		name         string
		noteRef      string
		section      string
	}{
		{"income", "Offering", "4", ""},
		{"income", "Tithe", "3", ""},
		{"income", "V.T.O.", "4", ""},
		{"income", "Donations Received", "6", ""},
		{"income", "Revivals", "4", ""},
		{"income", "Harvest Proceeds", "5", ""},
		{"income", "Other Incomes", "8", ""},
		{"income", "Group Almanac Days", "8", ""},
		{"income", "Donation for Manse", "6", ""},
		{"expenditure", "Repairs & Maintenance", "19", ""},
		{"expenditure", "Salaries & Allowances", "11", ""},
		{"expenditure", "Donations", "17", ""},
		{"expenditure", "Harvest Expenses", "5", ""},
		{"expenditure", "Stationery", "20", ""},
		{"expenditure", "Assessment", "9", ""},
		{"expenditure", "Evangelism", "13", ""},
		{"expenditure", "Operating Expenses", "20", ""},
		{"expenditure", "Conferences/Training & Retreat", "16", ""},
		{"expenditure", "Children Service Expense", "14", ""},
		{"expenditure", "J.Y. Expense", "14", ""},
		{"expenditure", "Utilities", "20", ""},
		{"expenditure", "Other Expenditure", "20", ""},
		{"asset", "Property, Plant & Equipment", "21", "non_current_asset"},
		{"asset", "GAP Presbytery", "22", "non_current_asset"},
		{"asset", "Investment (Credit Union)", "22", "non_current_asset"},
		{"asset", "Receivables (Debtors)", "25", "current_asset"},
		{"asset", "Bank", "26", "current_asset"},
		{"asset", "Cash", "26", "current_asset"},
		{"asset", "Momo", "26", "current_asset"},
		{"liability", "Payables (Creditors)", "28", "current_liability"},
		{"liability", "District Assessment Owing", "28", "current_liability"},
	}

	for _, mapping := range mappings {
		if _, err := DB.Exec(`
			UPDATE categories
			SET note_ref = ?, report_section = ?
			WHERE type = ? AND name = ?
		`, mapping.noteRef, mapping.section, mapping.categoryType, mapping.name); err != nil {
			return fmt.Errorf("update legacy category %s: %w", mapping.name, err)
		}
	}

	// Child accounts inherit their parent's note by design throughout the application. This
	// also repairs old offering and harvest children whose original Note 1–9 references would
	// otherwise disappear from the updated Notes 3–28 report after an upgrade.
	if _, err := DB.Exec(`
		UPDATE categories AS child
		SET note_ref = COALESCE((
			SELECT parent.note_ref FROM categories parent WHERE parent.id = child.parent_id
		), child.note_ref),
			report_section = ''
		WHERE child.parent_id <> 0
	`); err != nil {
		return fmt.Errorf("inherit updated parent metadata: %w", err)
	}
	return nil
}

// migrateLegacyCashHierarchy reparents the three cash accounts used by earlier releases
// beneath the workbook's Cash & Cash Equivalents line. seedCategories may just have created
// an empty child with the same name, so that placeholder is removed only when it has no
// financial references. If both rows contain data, both are preserved rather than risking
// an unsafe merge; the legacy top-level line will continue to report independently.
func migrateLegacyCashHierarchy() error {
	var cashParentID int64
	if err := DB.QueryRow(`
		SELECT id FROM categories
		WHERE type = 'asset' AND name = 'Cash & Cash Equivalents' AND parent_id = 0
	`).Scan(&cashParentID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	for _, name := range []string{"Bank", "Cash", "Momo"} {
		var legacyID int64
		if err := DB.QueryRow(`
			SELECT id FROM categories WHERE type = 'asset' AND name = ? AND parent_id = 0
		`, name).Scan(&legacyID); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}

		var childID int64
		err := DB.QueryRow(`
			SELECT id FROM categories WHERE type = 'asset' AND name = ? AND parent_id = ?
		`, name, cashParentID).Scan(&childID)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			var referenceCount int
			if err := DB.QueryRow(`
				SELECT
					(SELECT COUNT(*) FROM transactions WHERE category_id = ?) +
					(SELECT COUNT(*) FROM budgets WHERE category_id = ?) +
					(SELECT COUNT(*) FROM account_opening_balances WHERE category_id = ?) +
					(SELECT COUNT(*) FROM fixed_asset_openings WHERE category_id = ?)
			`, childID, childID, childID, childID).Scan(&referenceCount); err != nil {
				return err
			}
			if referenceCount != 0 {
				continue
			}
			if _, err := DB.Exec(`DELETE FROM categories WHERE id = ?`, childID); err != nil {
				return err
			}
		}

		if _, err := DB.Exec(`
			UPDATE categories
			SET parent_id = ?, note_ref = '26', report_section = ''
			WHERE id = ?
		`, cashParentID, legacyID); err != nil {
			return err
		}
	}
	return nil
}

func defaultCategoryReportSection(categoryType, name, parent string) string {
	if parent != "" {
		return ""
	}
	if categoryType == "liability" {
		return "current_liability"
	}
	if categoryType != "asset" {
		return ""
	}

	switch name {
	case "Property, Plant & Equipment", "GAP Presbytery", "Investment (Credit Union)":
		return "non_current_asset"
	case "Receivables (Debtors)", "Bank", "Cash", "Momo":
		return "current_asset"
	default:
		return ""
	}
}
