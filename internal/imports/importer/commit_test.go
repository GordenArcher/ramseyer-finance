package importer

import (
	"encoding/json"
	"errors"
	"math"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/imports/plan"
	"strconv"
	"testing"
	"time"
)

func TestCommitWritesBatchTransactionsAndAudit(t *testing.T) {
	setupTestDB(t)
	tithe := incomeCategory(t, "Tithes")

	built := &plan.Plan{
		FileName: "SUNDAY OFFERTORY.xlsx",
		FileHash: "first-hash",
		Year:     2026,
		Rows: []plan.Row{
			{
				Date:   time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC),
				Label:  "Tithe",
				Key:    "TITHES",
				Amount: 100,
				Sheet:  "08MAR",
				Line:   2,
			},
		},
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe", Rows: 1, Total: 100, Category: "Tithes", Source: plan.CategoryStandard},
		},
	}
	store := NewPreviewStore()
	token, err := store.Put(built)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	result, err := Commit(store, token, built, map[string]string{
		"TITHES": strconv.FormatInt(tithe.ID, 10),
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Year != 2026 || result.RowCount != 1 || result.Total != 100 || result.ReplacedRows != 0 {
		t.Fatalf("result = %+v", result)
	}

	var (
		fileName string
		fileHash string
		rowCount int
		total    float64
	)
	if err := db.DB.QueryRow(`
		SELECT file_name, file_hash, row_count, total_amount
		FROM import_batches WHERE id = ?
	`, result.BatchID).Scan(&fileName, &fileHash, &rowCount, &total); err != nil {
		t.Fatalf("read import batch: %v", err)
	}
	if fileName != built.FileName || fileHash != built.FileHash || rowCount != 1 || total != 100 {
		t.Fatalf("batch = %s/%s/%d/%.2f", fileName, fileHash, rowCount, total)
	}

	var (
		transactionID int64
		date          string
		transaction   string
		category      string
		categoryID    int64
		payment       string
		description   string
		amount        float64
		noteRef       string
		batchID       int64
	)
	if err := db.DB.QueryRow(`
		SELECT id, date, type, category, category_id, payment_method, description, amount, note_ref, import_batch_id
		FROM transactions
	`).Scan(&transactionID, &date, &transaction, &category, &categoryID, &payment, &description, &amount, &noteRef, &batchID); err != nil {
		t.Fatalf("read imported transaction: %v", err)
	}
	if date != "2026-01-04" || transaction != "income" || category != "Tithes" || categoryID != tithe.ID {
		t.Fatalf("transaction = %s/%s/%s/%d", date, transaction, category, categoryID)
	}
	if payment != "cash" || description != "Tithe" || amount != 100 || noteRef != tithe.NoteRef || batchID != result.BatchID {
		t.Fatalf("transaction details = %s/%s/%.2f/%s/%d", payment, description, amount, noteRef, batchID)
	}

	var actor, action string
	if err := db.DB.QueryRow(`
		SELECT action, actor FROM transaction_audit_log WHERE transaction_id = ?
	`, transactionID).Scan(&action, &actor); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if action != "created" || actor != importActor {
		t.Fatalf("audit = %s/%s", action, actor)
	}

	rawMap, err := db.GetSetting(labelMapSetting)
	if err != nil {
		t.Fatalf("read saved label map: %v", err)
	}
	var saved map[string]int64
	if err := json.Unmarshal([]byte(rawMap), &saved); err != nil {
		t.Fatalf("decode saved label map: %v", err)
	}
	if saved["TITHES"] != tithe.ID {
		t.Fatalf("saved mapping = %v, want TITHES=%d", saved, tithe.ID)
	}

	if _, ok := store.Get(token); ok {
		t.Fatal("confirmed preview should be removed from staging")
	}
}

func TestCommitReplacesThePreviousYearBatch(t *testing.T) {
	setupTestDB(t)
	tithe := incomeCategory(t, "Tithes")
	offertory := incomeCategory(t, "Adult Service Offertory")

	first := &plan.Plan{
		FileName: "first.xlsx",
		FileHash: "first-hash",
		Year:     2026,
		Rows: []plan.Row{
			{Date: time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC), Label: "Tithe", Key: "TITHES", Amount: 100},
		},
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe", Rows: 1, Total: 100, Category: "Tithes", Source: plan.CategoryStandard},
		},
	}
	store := NewPreviewStore()
	firstToken, err := store.Put(first)
	if err != nil {
		t.Fatalf("Put first: %v", err)
	}
	firstResult, err := Commit(store, firstToken, first, map[string]string{
		"TITHES": strconv.FormatInt(tithe.ID, 10),
	})
	if err != nil {
		t.Fatalf("Commit first: %v", err)
	}
	var firstTransactionID int64
	if err := db.DB.QueryRow(`SELECT id FROM transactions`).Scan(&firstTransactionID); err != nil {
		t.Fatalf("read first transaction: %v", err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO transactions (date, type, category, category_id, description, amount)
		VALUES ('2026-01-04', 'income', 'Tithes', ?, 'Manual entry', 25)
	`, tithe.ID); err != nil {
		t.Fatalf("insert manual transaction: %v", err)
	}

	second := &plan.Plan{
		FileName: "second.xlsx",
		FileHash: "second-hash",
		Year:     2026,
		Rows: []plan.Row{
			{Date: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC), Label: "Tithe", Key: "TITHES", Amount: 120},
			{Date: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC), Label: "Sunday Service Offering", Key: "ADULT SERVICE OFFERTORY", Amount: 30},
		},
		Labels: []plan.Label{
			{Key: "TITHES", Display: "Tithe", Rows: 1, Total: 120, Category: "Tithes", Source: plan.CategoryStandard},
			{Key: "ADULT SERVICE OFFERTORY", Display: "Sunday Service Offering", Rows: 1, Total: 30, Category: "Adult Service Offertory", Source: plan.CategoryStandard},
		},
	}
	secondToken, err := store.Put(second)
	if err != nil {
		t.Fatalf("Put second: %v", err)
	}
	secondResult, err := Commit(store, secondToken, second, map[string]string{
		"TITHES":                  strconv.FormatInt(tithe.ID, 10),
		"ADULT SERVICE OFFERTORY": strconv.FormatInt(offertory.ID, 10),
	})
	if err != nil {
		t.Fatalf("Commit second: %v", err)
	}
	if secondResult.ReplacedRows != 1 {
		t.Fatalf("replaced rows = %d, want 1", secondResult.ReplacedRows)
	}
	if secondResult.BatchID == firstResult.BatchID {
		t.Fatal("replacement should create a new batch")
	}

	var batches, transactions int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM import_batches`).Scan(&batches); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&transactions); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if batches != 1 || transactions != 3 {
		t.Fatalf("batches/transactions = %d/%d, want 1/3 including the manual entry", batches, transactions)
	}

	var manualExists int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions WHERE import_batch_id IS NULL`).Scan(&manualExists); err != nil {
		t.Fatalf("count manual transactions: %v", err)
	}
	if manualExists != 1 {
		t.Fatalf("manual transactions = %d, want the manual entry preserved", manualExists)
	}

	var oldExists int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions WHERE id = ?`, firstTransactionID).Scan(&oldExists); err != nil {
		t.Fatalf("count old transaction: %v", err)
	}
	if oldExists != 0 {
		t.Fatal("old batch transactions should be deleted")
	}

	var deletedActor, deletedAction string
	var deletedTransactionID int64
	if err := db.DB.QueryRow(`
		SELECT transaction_id, action, actor
		FROM transaction_audit_log
		WHERE action = 'deleted'
	`).Scan(&deletedTransactionID, &deletedAction, &deletedActor); err != nil {
		t.Fatalf("read deletion audit: %v", err)
	}
	if deletedTransactionID != firstTransactionID || deletedActor != importActor {
		t.Fatalf("deletion audit = %d/%s/%s", deletedTransactionID, deletedAction, deletedActor)
	}

	var createdAudits int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*) FROM transaction_audit_log WHERE action = 'created' AND actor = ?
	`, importActor).Scan(&createdAudits); err != nil {
		t.Fatalf("count created audits: %v", err)
	}
	if createdAudits != 3 {
		t.Fatalf("created audits = %d, want 3", createdAudits)
	}

	var total float64
	if err := db.DB.QueryRow(`SELECT total_amount FROM import_batches`).Scan(&total); err != nil {
		t.Fatalf("read batch total: %v", err)
	}
	if math.Abs(total-150) > 0.001 {
		t.Fatalf("batch total = %.2f, want 150", total)
	}
}

func TestCommitRefusesUnresolvedLabelsWithoutWriting(t *testing.T) {
	setupTestDB(t)

	built := &plan.Plan{
		FileName: "undecided.xlsx",
		FileHash: "hash",
		Year:     2026,
		Rows: []plan.Row{
			{Date: time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC), Label: "Day Born", Key: "DAY BORN", Amount: 50},
		},
		Labels: []plan.Label{
			{Key: "DAY BORN", Display: "Day Born", Rows: 1, Total: 50, Source: plan.CategoryUndecided},
		},
	}
	store := NewPreviewStore()
	token, err := store.Put(built)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, err = Commit(store, token, built, map[string]string{"DAY BORN": ""})
	var mappingErr *MappingError
	if !errors.As(err, &mappingErr) {
		t.Fatalf("error = %v, want *MappingError", err)
	}
	if !mappingErr.UnresolvedKeys["DAY BORN"] {
		t.Fatalf("unresolved = %v, want DAY BORN", mappingErr.UnresolvedKeys)
	}

	var batches, transactions int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM import_batches`).Scan(&batches); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&transactions); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if batches != 0 || transactions != 0 {
		t.Fatalf("batches/transactions = %d/%d, want nothing written", batches, transactions)
	}
	if _, ok := store.Get(token); !ok {
		t.Fatal("a failed confirmation should keep the preview available")
	}
}
