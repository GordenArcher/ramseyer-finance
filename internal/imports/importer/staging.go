package importer

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"ramseyer-finance/internal/imports/plan"
	"sync"
	"time"
)

const (
	// previewTTL is how long an uploaded workbook stays available for confirmation. The
	// operator reads the reconciliation report and assigns accounts in this window.
	previewTTL = 30 * time.Minute
	// maxStagedPreviews caps memory use. Each entry holds one parsed workbook, and a single
	// operator rarely needs more than one or two previews at a time.
	maxStagedPreviews = 20
	// previewTokenBytes is the entropy behind an unguessable token. The token only protects a
	// local screen, but a short random value is still cheaper than a sequential ID.
	previewTokenBytes = 32
)

// stagedPreview is one parsed workbook waiting for confirmation. The parsed plan is kept
// instead of the uploaded bytes so the commit does not have to read the workbook twice.
type stagedPreview struct {
	plan     *plan.Plan
	storedAt time.Time
}

// PreviewStore keeps parsed uploads between the preview and commit requests. It is an
// in-memory cache, not a database table: a restart should discard an unconfirmed import
// rather than resume writing rows the operator can no longer see.
type PreviewStore struct {
	mu      sync.Mutex
	entries map[string]stagedPreview
	ttl     time.Duration
	max     int
	now     func() time.Time
}

// NewPreviewStore creates the store with the application defaults.
func NewPreviewStore() *PreviewStore {
	return newPreviewStore(previewTTL, maxStagedPreviews, time.Now)
}

func newPreviewStore(ttl time.Duration, max int, now func() time.Time) *PreviewStore {
	return &PreviewStore{
		entries: make(map[string]stagedPreview),
		ttl:     ttl,
		max:     max,
		now:     now,
	}
}

// Put stores a parsed workbook and returns the token the confirmation form must send back.
// Expired entries are removed first, and the oldest entry is dropped when the store is
// already full so an abandoned preview cannot block the next upload.
func (s *PreviewStore) Put(built *plan.Plan) (string, error) {
	token, err := newPreviewToken()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.removeExpired(now)
	s.makeRoom()
	s.entries[token] = stagedPreview{plan: built, storedAt: now}
	return token, nil
}

// Get returns the parsed workbook for a token. An expired entry is removed on the way out,
// which keeps a forgotten preview from surviving until the next upload.
func (s *PreviewStore) Get(token string) (*plan.Plan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.removeExpired(s.now())
	entry, exists := s.entries[token]
	if !exists {
		return nil, false
	}
	return entry.plan, true
}

// Delete removes a confirmed preview. The handler can call it after a successful commit
// without waiting for the TTL.
func (s *PreviewStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, token)
}

func (s *PreviewStore) removeExpired(now time.Time) {
	for token, entry := range s.entries {
		if now.Sub(entry.storedAt) >= s.ttl {
			delete(s.entries, token)
		}
	}
}

func (s *PreviewStore) makeRoom() {
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

func newPreviewToken() (string, error) {
	payload := make([]byte, previewTokenBytes)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("generate import preview token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
