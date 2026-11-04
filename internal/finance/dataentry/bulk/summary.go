package bulk

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
}

// AppendRowError records one refusal with the row number the operator sees in Excel.
func (s *ImportSummary) AppendRowError(rowIndex int, message string) {
	if len(s.Errors) >= RowErrorLimit {
		return
	}
	// Row numbers are reported 1 based because that is what the operator sees in Excel.
	s.Errors = append(s.Errors, "Row "+strconv.Itoa(rowIndex+1)+": "+message)
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
