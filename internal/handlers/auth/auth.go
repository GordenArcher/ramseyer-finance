package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/startupstate"
	"ramseyer-finance/internal/webui"
	"strings"
	"sync"
	"time"
)

// Authentication constants define the cookie name used for session tracking and the
// database setting keys that persist the hashed PIN, its salt, and a version marker.
// The version key allows future credential format migrations without breaking existing
// databases—new code can check the version and upgrade or reject accordingly.
const (
	authCookieName = "ramseyer_finance_session"
	pinHashKey     = "auth.pin_hash"
	pinSaltKey     = "auth.pin_salt"
	pinVersionKey  = "auth.pin_version"
)

// sessionStore holds all active authentication sessions in an in-memory map protected
// by a mutex. Each session maps a cryptographically random token (stored in a cookie)
// to an expiration time. Because this is a single-user local desktop application, an
// in-process store is sufficient—an app restart naturally invalidates all sessions,
// and there is no need for persistent session storage across restarts.
var sessionStore = struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}{
	sessions: map[string]time.Time{},
}

// LoginData carries the template variables for the login page. Mode determines whether
// the UI shows a PIN creation form ("setup") or a PIN entry form ("unlock"). Message
// and MessageTone provide user-facing feedback after redirects (e.g., incorrect PIN,
// successful logout). HasPassword tells the template whether a PIN has been previously
// configured, which controls the visibility of the unlock input.
type LoginData struct {
	Mode        string
	Message     string
	MessageTone string
	HasPassword bool
}

// LoginPage serves the combined PIN setup / unlock screen. It inspects the database to
// determine whether the application is running for the first time (no PIN configured) or
// returning to an existing installation, then renders the appropriate form. If the user
// is already authenticated, they are redirected straight to the dashboard to avoid showing
// the login page unnecessarily.
func LoginPage(w http.ResponseWriter, r *http.Request) {
	// I decide between first-run setup and normal unlock on the server because persisted PIN state
	// is authoritative here. The UI should reflect the real credential state, not a client guess.
	requiresSetup, err := pinSetupRequired()
	if err != nil {
		serverError(w, err)
		return
	}

	// Avoid showing the login page at all when the user already holds a valid session
	// cookie—redirect them directly to the home page for a faster, cleaner flow.
	if !requiresSetup && isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	data := LoginData{
		Message:     r.URL.Query().Get("msg"),
		MessageTone: alertTone(r.URL.Query().Get("msg")),
		HasPassword: !requiresSetup,
	}
	if requiresSetup {
		data.Mode = "setup"
	} else {
		data.Mode = "unlock"
	}

	webui.RenderStandaloneTemplate(w, "login", data)
}

// SetupPIN handles the initial PIN creation POST request. It is only callable when no PIN
// exists in the database—once a PIN is configured, further changes must go through the
// authenticated ChangePIN handler. The function validates the PIN format, generates a
// fresh salt, hashes the PIN with iterative stretching, persists all three settings
// (hash, salt, version), creates a session so the user doesn't need to log in immediately
// after setup, and redirects to the one-time financial-record choice.
func SetupPIN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// I block setup once a PIN already exists because credential rotation should happen through the
	// authenticated change-PIN flow, not by replaying initialization.
	requiresSetup, err := pinSetupRequired()
	if err != nil {
		serverError(w, err)
		return
	}
	if !requiresSetup {
		http.Redirect(w, r, "/login?msg=PIN+already+configured", http.StatusSeeOther)
		return
	}

	pin := strings.TrimSpace(r.FormValue("pin"))
	if err := validatePIN(pin); err != nil {
		http.Redirect(w, r, "/login?msg="+queryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	if err := persistPIN(pin); err != nil {
		serverError(w, err)
		return
	}
	// PIN creation is the fresh-install boundary. Marking the record choice pending here means
	// only a genuinely new installation sees the prompt; upgraded databases without this marker
	// continue normally instead of being treated as new.
	if err := startupstate.MarkPending(); err != nil {
		serverError(w, err)
		return
	}

	// Automatically log the user in after initial PIN creation so they don't land on the
	// dashboard only to be prompted for the PIN they just created.
	if err := createSession(w); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/startup", http.StatusSeeOther)
}

// Unlock handles the PIN entry POST request for returning users. It validates the provided
// PIN against the stored hash using constant-time comparison to prevent timing side-channel
// attacks. On success, it creates a new session and redirects to the startup choice; on
// failure, it redirects back to the login page with an error message.
func Unlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// I re-check whether setup is still required here instead of trusting the rendered page mode,
	// because the persisted auth state may have changed since the page was opened.
	requiresSetup, err := pinSetupRequired()
	if err != nil {
		serverError(w, err)
		return
	}
	if requiresSetup {
		http.Redirect(w, r, "/login?msg=Create+a+PIN+first", http.StatusSeeOther)
		return
	}

	ok, err := verifyPIN(strings.TrimSpace(r.FormValue("pin")))
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		http.Redirect(w, r, "/login?msg=Incorrect+PIN", http.StatusSeeOther)
		return
	}

	decisionRequired, err := startupstate.DecisionRequired()
	if err != nil {
		serverError(w, err)
		return
	}
	if err := createSession(w); err != nil {
		serverError(w, err)
		return
	}
	if decisionRequired {
		http.Redirect(w, r, "/startup", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout destroys the current session by removing it from the in-memory store and clearing
// the browser cookie. The MaxAge=-1 cookie instructs the browser to delete it immediately.
// After logout, the user is redirected to the login page with a confirmation message.
func Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Remove the session token from the server-side store so even if someone captured
	// the cookie value, it cannot be replayed after logout.
	if cookie, err := r.Cookie(authCookieName); err == nil {
		sessionStore.mu.Lock()
		delete(sessionStore.sessions, cookie.Value)
		sessionStore.mu.Unlock()
	}

	// Overwrite the browser's cookie with an expired version that has the same path and
	// security attributes. MaxAge=-1 tells the browser to delete it immediately rather
	// than waiting for the session to end.
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login?msg=Signed+out", http.StatusSeeOther)
}

// ChangePIN allows an authenticated user to rotate their PIN from within the dashboard
// settings page. It requires the current PIN as proof of identity (there is no separate
// recovery mechanism in this local app), validates the new PIN format and that the
// confirmation matches, then persists the new credentials and redirects back with a
// status message.
func ChangePIN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	// I require the current PIN before allowing a change because there is no separate recovery
	// channel in this local app. Without that check, the lock screen would be trivial to bypass.
	currentPIN := strings.TrimSpace(r.FormValue("current_pin"))
	newPIN := strings.TrimSpace(r.FormValue("new_pin"))
	confirmPIN := strings.TrimSpace(r.FormValue("confirm_pin"))
	// Confirm the new PIN was typed correctly before touching any stored state—this is a
	// pure UX guard that catches typos early and avoids locking the user out.
	if newPIN != confirmPIN {
		http.Redirect(w, r, "/setup?msg=New+PIN+entries+did+not+match", http.StatusSeeOther)
		return
	}
	if err := validatePIN(newPIN); err != nil {
		http.Redirect(w, r, "/setup?msg="+queryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	ok, err := verifyPIN(currentPIN)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		http.Redirect(w, r, "/setup?msg=Current+PIN+is+incorrect", http.StatusSeeOther)
		return
	}

	// Generate a brand-new salt when the PIN changes so that identical new PINs do not
	// produce identical stored hashes, either within this database or across different
	// installations.
	if err := persistPIN(newPIN); err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=PIN+updated", http.StatusSeeOther)
}

// WithAuth is an HTTP middleware that protects routes behind PIN authentication. It checks
// two conditions in order: first whether a PIN has been configured at all (if not, redirect
// to setup), then whether the current request carries a valid, non-expired session cookie.
// Both checks must pass before the wrapped handler is invoked. This middleware is applied to
// every route except the login page, the unlock/setup POST handlers, and static assets.
func WithAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// I gate on setup state before session state so a first-run database cannot drift into a
		// protected screen through an old cookie or stale browser state.
		requiresSetup, err := pinSetupRequired()
		if err != nil {
			serverError(w, err)
			return
		}

		if requiresSetup {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if !isAuthenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

// pinSetupRequired checks whether both the PIN hash and salt settings exist in the database.
// If either is missing or empty, the application treats itself as unconfigured and shows the
// setup flow. This function does not validate the format of the stored values—it only
// confirms their presence, leaving format and version checks to verifyPIN.
func pinSetupRequired() (bool, error) {
	hash, err := db.GetSetting(pinHashKey)
	if err != nil {
		return false, err
	}
	salt, err := db.GetSetting(pinSaltKey)
	if err != nil {
		return false, err
	}
	return hash == "" || salt == "", nil
}

// isAuthenticated validates the session cookie from an incoming request. It extracts the
// token, looks it up in the in-memory session store, checks for expiration, and—if valid—
// extends the session by another 24 hours. This sliding-expiration approach means active
// users stay logged in indefinitely, while abandoned sessions clean themselves up after
// 24 hours of inactivity.
func isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(authCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}

	sessionStore.mu.Lock()
	defer sessionStore.mu.Unlock()

	expiresAt, ok := sessionStore.sessions[cookie.Value]
	if !ok {
		return false
	}
	// Check for natural expiration before extending. If the session has already expired,
	// remove it from the store to prevent accumulating dead entries.
	if time.Now().After(expiresAt) {
		delete(sessionStore.sessions, cookie.Value)
		return false
	}

	// Extend the session's lifetime on every authenticated request so the user stays
	// logged in as long as they remain active. Without this sliding window, a user
	// who starts a long data-entry session could be logged out mid-flow.
	sessionStore.sessions[cookie.Value] = time.Now().Add(24 * time.Hour)
	return true
}

// createSession generates a new cryptographically random session token, stores it in the
// in-memory session map with a 24-hour expiration, and sets it as an HttpOnly cookie on
// the response. HttpOnly prevents JavaScript from reading the cookie, mitigating XSS-based
// session theft. SameSiteLaxMode allows the cookie on top-level navigations but blocks it
// on cross-site subrequests, which is a reasonable default for a single-page local app.
func createSession(w http.ResponseWriter) error {
	// I keep sessions server-side in memory because this is a single-user local desktop app.
	// That keeps the cookie opaque and lets an app restart invalidate stale sessions naturally.
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	sessionStore.mu.Lock()
	sessionStore.sessions[token] = time.Now().Add(24 * time.Hour)
	sessionStore.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	return nil
}

// validatePIN enforces the PIN format rules: between 4 and 8 characters, all digits.
// A minimum of 4 digits provides a basic brute-force barrier (10,000 combinations), while
// the maximum of 8 keeps the PIN usable on a keypad-style lock screen. The function returns
// a human-readable error suitable for display in the UI.
func validatePIN(pin string) error {
	// I keep the PIN policy intentionally pragmatic: enough to prevent accidental weak entries,
	// but still short enough for the keypad-style lock screen this product uses.
	if len(pin) < 4 || len(pin) > 8 {
		return fmt.Errorf("PIN must be 4 to 8 digits")
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return fmt.Errorf("PIN must use digits only")
		}
	}
	return nil
}

// persistPIN generates a fresh 16-byte salt, hashes the provided PIN with 120,000 rounds
// of SHA-256 key stretching, and stores the salt, hash, and a version marker ("1") in the
// database settings table. A fresh salt is generated on every call (including PIN changes)
// so that identical PINs produce different stored representations, both over time and
// across different installations of the application.
func persistPIN(pin string) error {
	// I store a fresh salt and a version marker so the credential format can evolve later without
	// guessing how older databases hashed their PINs.
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return fmt.Errorf("generate PIN salt: %w", err)
	}

	salt := hex.EncodeToString(saltBytes)
	hash := hashPIN(pin, saltBytes)

	if err := db.SetSetting(pinSaltKey, salt); err != nil {
		return err
	}
	if err := db.SetSetting(pinHashKey, hex.EncodeToString(hash[:])); err != nil {
		return err
	}
	if err := db.SetSetting(pinVersionKey, "1"); err != nil {
		return err
	}
	return nil
}

// verifyPIN checks a candidate PIN against the stored hash. It first validates the PIN
// format (cheap rejection for obviously invalid input), retrieves the stored salt and hash
// from the database, recomputes the hash with the same salt and iteration count, and then
// uses crypto/subtle.ConstantTimeCompare to avoid leaking timing information that could
// help an attacker narrow down the correct PIN through repeated attempts.
func verifyPIN(pin string) (bool, error) {
	// I validate the shape first so obviously bad input fails cheaply before I touch stored state.
	if err := validatePIN(pin); err != nil {
		return false, nil
	}

	saltHex, err := db.GetSetting(pinSaltKey)
	if err != nil {
		return false, err
	}
	hashHex, err := db.GetSetting(pinHashKey)
	if err != nil {
		return false, err
	}
	// If the stored credentials are missing or incomplete, treat it as a configuration
	// error rather than an invalid PIN. This prevents a partially-written database from
	// accidentally allowing access.
	if saltHex == "" || hashHex == "" {
		return false, errors.New("PIN is not configured")
	}

	saltBytes, err := hex.DecodeString(saltHex)
	if err != nil {
		return false, fmt.Errorf("decode PIN salt: %w", err)
	}
	expectedHash, err := hex.DecodeString(hashHex)
	if err != nil {
		return false, fmt.Errorf("decode PIN hash: %w", err)
	}

	actualHash := hashPIN(pin, saltBytes)
	// ConstantTimeCompare returns 1 if the byte slices are equal, 0 otherwise. It
	// compares every byte regardless of where the first difference occurs, which
	// prevents an attacker from timing how long the comparison takes to deduce
	// how many leading bytes they guessed correctly.
	if subtle.ConstantTimeCompare(expectedHash, actualHash[:]) != 1 {
		return false, nil
	}
	return true, nil
}

// hashPIN computes a 32-byte SHA-256 hash of the concatenated salt and PIN, then
// iteratively re-hashes the result with the salt for 120,000 additional rounds. This
// key-stretching approach dramatically increases the cost of brute-force attacks: even
// with a small 4–8 digit PIN keyspace, an attacker must perform 120,001 hash operations
// per guess. The function allocates fresh slices on each iteration to avoid mutating
// shared state, at the cost of some GC pressure—acceptable for an infrequent operation
// like login.
func hashPIN(pin string, salt []byte) [32]byte {
	// I stretch the salted hash with repeated rounds because this is the only durable secret the
	// local lock screen depends on, and the extra CPU cost is acceptable at this scale.
	payload := append([]byte{}, salt...)
	payload = append(payload, []byte(pin)...)
	hash := sha256.Sum256(payload)
	// Each round prepends the original salt and appends the previous hash, then hashes
	// again. This prevents precomputation attacks because the salt is mixed in at every
	// step, and the chain is functionally irreversible without the PIN.
	for i := 0; i < 120000; i++ {
		nextInput := append([]byte{}, salt...)
		nextInput = append(nextInput, hash[:]...)
		hash = sha256.Sum256(nextInput)
	}
	return hash
}

// queryEscape sanitises a raw message string for inclusion in a URL query parameter. It
// replaces spaces with '+' characters (standard application/x-www-form-urlencoded encoding),
// strips out double-quotes, hash signs, and question marks to prevent parameter injection,
// and replaces ampersands with the word "and" to avoid breaking query parameter boundaries.
// This is a pragmatic, lightweight sanitizer rather than a complete URL encoder.
func queryEscape(raw string) string {
	replacer := strings.NewReplacer(
		" ", "+",
		"\"", "",
		"#", "",
		"&", "and",
		"?", "",
	)
	return replacer.Replace(raw)
}

// alertTone determines the CSS alert class for feedback messages on the login page. It
// inspects the message text for keywords that indicate an error or warning condition, and
// returns "warning" if any are found. Messages without warning markers—or an empty
// message—default to "success", which renders positive feedback like "PIN created" or
// "Signed out" in a green, non-alarming style.
func alertTone(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return "success"
	}

	// These case-insensitive markers cover the most common error and warning messages
	// produced by the authentication handlers. The list is deliberately kept small and
	// focused—general application errors that reach the login page should alert the
	// user, while successful operations should feel unobtrusive.
	for _, marker := range []string{
		"incorrect",
		"invalid",
		"did not",
		"must",
		"create",
		"already",
		"replace",
		"failed",
		"duplicate",
		"required",
		"no backup file",
	} {
		if strings.Contains(normalized, marker) {
			return "warning"
		}
	}

	return "success"
}

// serverError deliberately logs diagnostic detail while returning a generic message to the
// local webview. Authentication failures can contain storage details that should not become
// part of the operator-facing page.
func serverError(w http.ResponseWriter, err error) {
	log.Printf("authentication request failed: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

func badRequest(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusBadRequest)
}
