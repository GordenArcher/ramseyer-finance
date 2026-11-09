package report

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"sync"
	"time"
)

const (
	// ResultTTL keeps an import report long enough for the operator to read it after the
	// redirect. The report is a screen, not a record: the transactions themselves are saved.
	ResultTTL = 30 * time.Minute
	// MaxResults caps memory for abandoned uploads.
	MaxResults = 10
	// RowErrorLimit caps how many per-row messages travel back to the page. Beyond this the
	// operator is looking at a structurally wrong sheet, not individual bad cells.
	RowErrorLimit = 25
	// resultTokenBytes keeps the report token unguessable even though it only guards a
	// local summary screen.
	resultTokenBytes = 24
)

// ImportSummary is what the operator sees after an upload: how many rows landed, how
// many were refused, and the specific reason for each refusal. Silent skipping is the
// failure mode this struct exists to prevent, so every refused row must carry a message.
type ImportSummary struct {
	Sheet    string
	Imported int
	Skipped  int
	Errors   []string
	// Created lists the expense heads the upload added to the chart. Only expenditure
	// uploads create accounts, so the slice stays empty for the other types. Reporting the
	// names once lets the operator see exactly what the workbook introduced.
	Created []string
	// Sections is populated only by the all-types upload. Each entry is one type's share
	// of the submission, so the operator can tell which workbook landed and which one still
	// needs attention without opening four separate reports.
	Sections []ImportSection
}

// ImportSection is one transaction type's result inside an all-types upload. Errors stays
// on the section until the handler folds it into the combined report with the type label,
// which is what keeps "Row 4" from being ambiguous when four files were submitted.
type ImportSection struct {
	Type     string
	Label    string
	Sheet    string
	Imported int
	Skipped  int
	Errors   []string
	Created  []string
}

// AppendCreated records one added expense head without repeating a name that appeared on
// several rows. The all-types upload folds each section's list into the combined report, so
// the same dedupe rule serves both paths.
func (s *ImportSummary) AppendCreated(name string) {
	s.Created = appendCreated(s.Created, name)
}

// AppendCreated mirrors the summary method so a section can collect its own added heads
// before the combined report is assembled.
func (s *ImportSection) AppendCreated(name string) {
	s.Created = appendCreated(s.Created, name)
}

func appendCreated(names []string, name string) []string {
	if name == "" {
		return names
	}
	for _, existing := range names {
		if existing == name {
			return names
		}
	}
	return append(names, name)
}

// AppendRowError records one refusal with the row number the operator sees in Excel.
func (s *ImportSummary) AppendRowError(rowIndex int, message string) {
	s.Errors = appendRowError(s.Errors, rowIndex, message)
}

// AppendRowError mirrors the summary method so a section can collect its own refusals
// before the combined report is assembled.
func (s *ImportSection) AppendRowError(rowIndex int, message string) {
	s.Errors = appendRowError(s.Errors, rowIndex, message)
}

// appendRowError is the one place the row limit and the 1 based row label live. Both the
// single-type and all-types reports route through it so their messages cannot drift apart.
func appendRowError(errors []string, rowIndex int, message string) []string {
	if len(errors) >= RowErrorLimit {
		return errors
	}
	// Row numbers are reported 1 based because that is what the operator sees in Excel.
	return append(errors, "Row "+strconv.Itoa(rowIndex+1)+": "+message)
}

// storedResult is one import report waiting to be read by the data-entry page.
type storedResult struct {
	summary  ImportSummary
	storedAt time.Time
}

// SummaryStore holds import reports between the upload redirect and the page render.
// This mirrors the workbook preview store used by the Sunday offertory importer: an
// in-memory cache is correct here because a restart should discard a report the operator
// can no longer act on, never replay it.
type SummaryStore struct {
	mu      sync.Mutex
	entries map[string]storedResult
	ttl     time.Duration
	max     int
	now     func() time.Time
}

// NewSummaryStore builds a store with the given lifetime and capacity.
func NewSummaryStore(ttl time.Duration, max int, now func() time.Time) *SummaryStore {
	return &SummaryStore{
		entries: make(map[string]storedResult),
		ttl:     ttl,
		max:     max,
		now:     now,
	}
}

// Put stores one summary and returns the token the redirect carries.
func (s *SummaryStore) Put(summary ImportSummary) string {
	token, err := newResultToken()
	if err != nil {
		// A failed token must not block a successful import, so the caller gets an empty
		// token and the page simply shows no report.
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.removeExpired(now)
	s.makeRoom()
	s.entries[token] = storedResult{summary: summary, storedAt: now}
	return token
}

// Get returns a summary and removes it, because an import report is read once.
func (s *SummaryStore) Get(token string) (ImportSummary, bool) {
	if token == "" {
		return ImportSummary{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.removeExpired(s.now())
	entry, exists := s.entries[token]
	if !exists {
		return ImportSummary{}, false
	}
	delete(s.entries, token)
	return entry.summary, true
}

func (s *SummaryStore) removeExpired(now time.Time) {
	for token, entry := range s.entries {
		if now.Sub(entry.storedAt) >= s.ttl {
			delete(s.entries, token)
		}
	}
}

func (s *SummaryStore) makeRoom() {
	for len(s.entries) >= s.max && len(s.entries) > 0 {
		oldestToken := ""
		var oldest time.Time
		for token, entry := range s.entries {
			if oldestToken == "" || entry.storedAt.Before(oldest) {
				oldestToken = token
				oldest = entry.storedAt
			}
		}
		delete(s.entries, oldestToken)
	}
}

func newResultToken() (string, error) {
	payload := make([]byte, resultTokenBytes)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("generate bulk import token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
