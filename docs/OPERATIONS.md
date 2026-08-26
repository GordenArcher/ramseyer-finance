# Operations Guide

## Daily Use

### Unlock

Start the application and enter the configured PIN on the lock screen. The app opens to the dashboard.

### Enter Transactions

Click **+ New Entry** (or navigate to **Data Entry** from the sidebar) to open the transaction entry modal. Four tabs are available:

- **Income** — offerings, tithes, donations, harvest proceeds, and other inflows
- **Expenditure** — repairs, salaries, utilities, stationery, and other outflows
- **Asset** — property, equipment, bank movements, and receivables (description required)
- **Liability** — payables and assessment amounts owed (description required)

Each tab presents the relevant searchable category selector. Select a date, category, amount, and optional description, then save.

### Review and Correct Records

Use the **Transactions** register to:

- Browse all entries filtered by year, type, or keyword search
- Page through results (25 per page)
- Click **Edit** on any row to modify its fields inline
- Click **Delete** to remove a transaction (requires confirmation)
- View the recent activity sidebar showing the last 12 audited changes

### Configure Settings

Use the **Setup** screen to manage:

- **Budgets** — annual budget amounts per top-level income and expenditure category
- **Accumulated Fund** — opening fund and prior-year adjustments for the annual roll-forward
- **Balance-Sheet Openings** — brought-forward asset and liability account positions
- **Fixed-Asset Openings** — opening cost and accumulated depreciation by Note 21 class
- **Legacy Cash Openings** — existing Bank, Cash, and Momo opening records retained from earlier releases
- **PIN Change** — rotate the application lock PIN (requires current PIN)
- **Auto-Backup** — enable/disable and set frequency

## Backup Strategy

### Manual Backup

1. Navigate to the **Backup** screen.
2. Click **Download Backup**.
3. The saved file path is displayed in the UI after completion.

Manual backups are stored in the Downloads folder with the current date in the filename.

### Automatic Backup

Configure from **Setup → Auto-Backup Settings**.

| Frequency | Interval | Retention | Best For |
|-----------|----------|-----------|----------|
| Weekly | Every 7 days | Keeps last 12 | Frequent changes, weekly reporting |
| Monthly | Every 30 days | Keeps last 12 | Moderate activity, monthly close |
| Quarterly | Every 90 days | Keeps last 8 | Low activity, annual reporting focus |
| Off | — | — | Manual-only backup strategy |

The scheduler:

- Runs an immediate check on application startup to catch missed backups
- Continues checking every 12 hours while the app stays open
- Stores backup files in `Ramseyer Finance Backups/Auto/` with timestamped filenames
- Prunes old backups automatically according to the retention limit

### Restore

1. Navigate to the **Backup** screen.
2. Search or filter the in-app backup library and select a restore point. For an external backup, drag and drop it onto the restore panel.
3. Click **Restore** and confirm the action.
4. The app creates a **safety backup** of the current database before replacing it.
5. After restore completes, the app reloads with the restored data.

If the wrong backup was selected, find the safety backup in `Ramseyer Finance Backups/Safety/` and restore that file to revert.

## Export Strategy

Exports are available from the **Transactions** register page. Use the export panel to:

1. Set a date range (defaults to the currently viewed year).
2. Optionally filter by transaction type.
3. Choose a format and click export.

### Export Formats

| Format | File Extension | Use Case |
|--------|---------------|----------|
| CSV | `.csv` | Import into spreadsheet applications or other accounting tools |
| Excel | `.xls` | Open directly in Microsoft Excel with formatted headers |
| PDF | `.pdf` | Printable archival report with fixed-width text layout |

Exports are saved to the Downloads folder. The UI displays the full saved path after completion.

## Audit Trail

Every transaction change is recorded in an audit log. The **Transactions** register shows the 12 most recent audit events in the sidebar.

Audited actions:

| Action | When It Is Recorded |
|--------|-------------------|
| Created | After a new transaction is saved |
| Updated | After an existing transaction is edited and saved |
| Deleted | After a transaction is removed (snapshot captured before deletion) |

Each audit entry stores a full JSON snapshot of the transaction at the time of the action, including category name, note reference, description, amount, and timestamps. This snapshot is independent of the current transaction data—deleted transactions remain visible in the audit trail.

## Reports

The application includes the workbook-aligned statements and supporting reports below:

| Report | Purpose |
|--------|---------|
| **Monthly** | Month-by-month breakdown of income, expenditure, and surplus with YTD totals |
| **Quarterly** | Calendar quarter aggregation (Q1–Q4) with full-year totals |
| **Financial Performance** | Trial Balance note totals for income, expenditure, surplus, budget variance, and accumulated fund |
| **Trial Balance** | The editable, year-owned source for Notes and year-end statements, with a debit/credit control difference |
| **Financial Position** | Trial Balance assets, liabilities, accumulated fund, and balance difference |
| **Cash Flow** | Indirect flows derived from current/prior Trial Balance notes and reconciled to Note 26 |
| **Fixed Assets** | Note 21 cost, depreciation/amortization, and carrying-value schedule |
| **Notes** | Comparative details linked directly to each year's Trial Balance rows for workbook Notes 3–28 |

Each Trial Balance year owns its account rows, types, note mappings, and debit/credit values. Editing or adding a row in one year does not change another year. On upgrade, the first visit to a year creates a one-time snapshot from the existing transaction/opening data; after that point the saved Trial Balance is authoritative for Notes and year-end statements.

All reports support year selection and include comparative prior-year figures where applicable. **Save PDF & Open** generates a real PDF in Downloads and opens it in the operating system's PDF viewer, where it can be printed normally. Reconciliation warnings remain visible until the saved Trial Balance is corrected.

## Database Notes

- The application uses SQLite with WAL journal mode for concurrent read performance.
- The database is initialised with the full schema and any pending migrations at startup.
- Backups use SQLite's `VACUUM INTO` command rather than copying the live database file directly. This produces a consistent, self-contained snapshot without requiring separate handling of WAL and SHM sidecar files.
- Database sidecar files (`-wal` and `-shm`) are cleaned up during restore to prevent stale journal data from contaminating the restored database.

## Recovery Notes

If a restore produces incorrect results:

1. Navigate to the backup storage folder (see [Data Locations](README.md#default-backup-and-export-paths)).
2. Open `Ramseyer Finance Backups/Safety/`.
3. Find the most recent file with the `pre-restore` prefix.
4. Return to the app's **Backup** screen.
5. Select that safety file and restore it.

The safety backup is an exact copy of the database as it existed immediately before the restore operation. Restoring the safety backup returns the application to its pre-restore state.
