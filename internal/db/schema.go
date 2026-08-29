package db

const schema = `
CREATE TABLE IF NOT EXISTS transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('income','expenditure','asset','liability')),
    category TEXT NOT NULL,
    category_id INTEGER,
    counter_category_id INTEGER,
    payment_method TEXT NOT NULL DEFAULT 'cash',
    subcategory TEXT DEFAULT '',
    description TEXT DEFAULT '',
    amount REAL NOT NULL,
    note_ref TEXT DEFAULT '',
    created_at TEXT DEFAULT (datetime('now','localtime')),
    updated_at TEXT DEFAULT (datetime('now','localtime')),
    FOREIGN KEY (category_id) REFERENCES categories(id),
    FOREIGN KEY (counter_category_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL CHECK(type IN ('income','expenditure','asset','liability')),
    name TEXT NOT NULL,
    parent_id INTEGER DEFAULT 0,
    note_ref TEXT DEFAULT '',
    report_section TEXT NOT NULL DEFAULT '',
    is_active INTEGER NOT NULL DEFAULT 1,
    UNIQUE(type, name, parent_id)
);

CREATE TABLE IF NOT EXISTS budgets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    year INTEGER NOT NULL,
    category_id INTEGER NOT NULL,
    amount REAL NOT NULL DEFAULT 0,
    FOREIGN KEY (category_id) REFERENCES categories(id),
    UNIQUE(year, category_id)
);

CREATE TABLE IF NOT EXISTS opening_balances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    year INTEGER NOT NULL,
    account_type TEXT NOT NULL CHECK(account_type IN ('bank','cash','momo')),
    amount REAL NOT NULL DEFAULT 0,
    UNIQUE(year, account_type)
);

CREATE TABLE IF NOT EXISTS fund_rollforwards (
    year INTEGER PRIMARY KEY,
    opening_balance REAL NOT NULL DEFAULT 0,
    prior_year_adjustment REAL NOT NULL DEFAULT 0,
    updated_at TEXT DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS account_opening_balances (
    year INTEGER NOT NULL,
    category_id INTEGER NOT NULL,
    amount REAL NOT NULL DEFAULT 0,
    updated_at TEXT DEFAULT (datetime('now','localtime')),
    PRIMARY KEY(year, category_id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS fixed_asset_openings (
    year INTEGER NOT NULL,
    category_id INTEGER NOT NULL,
    opening_cost REAL NOT NULL DEFAULT 0,
    opening_accumulated_depreciation REAL NOT NULL DEFAULT 0,
    updated_at TEXT DEFAULT (datetime('now','localtime')),
    PRIMARY KEY(year, category_id),
    FOREIGN KEY (category_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS trial_balance_years (
    year INTEGER PRIMARY KEY,
    initialized_at TEXT DEFAULT (datetime('now','localtime')),
    updated_at TEXT DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS trial_balance_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    year INTEGER NOT NULL,
    account_type TEXT NOT NULL CHECK(account_type IN ('income','expenditure','asset','liability','equity')),
    note_ref TEXT NOT NULL DEFAULT '',
    account_name TEXT NOT NULL,
    debit REAL NOT NULL DEFAULT 0 CHECK(debit >= 0),
    credit REAL NOT NULL DEFAULT 0 CHECK(credit >= 0),
    sort_order INTEGER NOT NULL DEFAULT 0,
    source_category_id INTEGER,
    created_at TEXT DEFAULT (datetime('now','localtime')),
    updated_at TEXT DEFAULT (datetime('now','localtime')),
    FOREIGN KEY (year) REFERENCES trial_balance_years(year) ON DELETE CASCADE,
    FOREIGN KEY (source_category_id) REFERENCES categories(id)
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS dashboard_greetings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message TEXT NOT NULL UNIQUE,
    is_active INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS transaction_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id INTEGER NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('created','updated','deleted')),
    actor TEXT NOT NULL DEFAULT 'local operator',
    snapshot_json TEXT NOT NULL DEFAULT '',
    created_at TEXT DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS backup_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    file_path TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT DEFAULT (datetime('now','localtime'))
);

CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions(date);
CREATE INDEX IF NOT EXISTS idx_transactions_type ON transactions(type);
CREATE INDEX IF NOT EXISTS idx_transactions_category ON transactions(category);
CREATE INDEX IF NOT EXISTS idx_transactions_type_date ON transactions(type, date);
CREATE INDEX IF NOT EXISTS idx_transactions_category_id ON transactions(category_id);
CREATE INDEX IF NOT EXISTS idx_transactions_category_id_date ON transactions(category_id, date);
CREATE INDEX IF NOT EXISTS idx_dashboard_greetings_active_sort ON dashboard_greetings(is_active, sort_order, id);
CREATE INDEX IF NOT EXISTS idx_transaction_audit_log_transaction_id ON transaction_audit_log(transaction_id);
CREATE INDEX IF NOT EXISTS idx_backup_events_created_at ON backup_events(created_at);
CREATE INDEX IF NOT EXISTS idx_trial_balance_entries_year_sort ON trial_balance_entries(year, sort_order, id);
CREATE INDEX IF NOT EXISTS idx_trial_balance_entries_year_note ON trial_balance_entries(year, note_ref, account_type);
`
