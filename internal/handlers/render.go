package handlers

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"strings"
)

// templateFS embeds all HTML template files directly into the compiled binary. The "templates/"
// directory contains the page-level templates (dashboard.html, transactions.html, etc.), and
// the "templates/partials/" directory contains reusable fragments (sidebar, modals, chart
// includes). Using embed means the application is a single self-contained binary with no
// external template file dependencies at runtime—no need to worry about missing template
// files or incorrect relative paths in production deployments.
//
//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

// funcMap defines the custom template functions available to all Go HTML templates rendered
// by this application. These functions are registered before parsing so they can be called
// directly within template expressions using the pipe syntax (e.g., {{ .Amount | formatMoney }}).
// Keeping formatting logic in Go rather than JavaScript ensures consistent number display
// across all pages and avoids locale-dependent formatting differences between browsers.
var funcMap = template.FuncMap{
	// formatMoney formats a float64 as a currency string with comma separators and exactly
	// two decimal places. Used for detailed financial displays like transaction amounts
	// and statement line items where precision is required.
	"formatMoney": formatMoney,
	// formatCompactMoney formats a float64 as a compact human-readable string (e.g., "1.5M"
	// instead of "1,500,000.00"). Used for summary cards on the dashboard where space is
	// limited. The exact value is still available via the raw number for tooltip display.
	"formatCompactMoney": formatCompactMoney,
	// formatMoneyRaw formats a float64 as a plain numeric string without commas but with
	// trailing zeros trimmed (e.g., "1500.50" becomes "1500.5"). Used when the value is
	// fed into JavaScript or data attributes rather than displayed directly.
	"formatMoneyRaw": formatMoneyRaw,
	// toJSON serialises a Go value to its JSON representation and returns it as a
	// template.JS value, which bypasses HTML escaping. This is essential for embedding
	// chart data or other structured data into <script> tags in the template. If
	// marshalling fails, it returns "null" rather than panicking, which is safe for
	// JavaScript consumption.
	"toJSON": func(v any) template.JS {
		payload, err := json.Marshal(v)
		if err != nil {
			return template.JS("null")
		}
		return template.JS(payload)
	},
	// negate returns the arithmetic negation of a float64. This is used in templates to
	// flip the sign of expenditure amounts for display (showing expenses as positive
	// numbers in certain contexts) without modifying the underlying data.
	"negate": func(f float64) float64 {
		return -f
	},
	// abs returns the absolute value of a float64. This is used when the template needs
	// to display a magnitude without regard to sign, such as in comparative columns
	// where the direction is already indicated by the column header.
	"abs": func(f float64) float64 {
		if f < 0 {
			return -f
		}
		return f
	},
}

// formatMoney formats a float64 as a human-readable currency string suitable for display
// in financial statements, transaction lists, and summary sections. It always produces
// exactly two decimal places and inserts comma separators between every three digits of
// the integer portion (e.g., 1234567.89 becomes "1,234,567.89"). Negative values are
// prefixed with a minus sign. This function deliberately avoids locale-specific formatting
// (no currency symbol, no locale-dependent separators) to keep the output consistent
// across all deployments.
func formatMoney(f float64) string {
	// I format money server-side so every template gets the same separators and decimal handling
	// without repeating number formatting logic in HTML or JavaScript.
	s := fmt.Sprintf("%.2f", f)
	parts := strings.Split(s, ".")
	intPart := parts[0]
	// Insert comma separators into the integer portion. The algorithm works from left to
	// right, inserting a comma before a digit when the number of digits remaining
	// (including the current one) is a multiple of 3 and we're not at the first digit.
	// This correctly handles negative signs as well, since the minus sign is the first
	// character and the comma insertion logic is position-relative.
	if len(intPart) > 3 {
		var result []byte
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				result = append(result, ',')
			}
			result = append(result, byte(c))
		}
		intPart = string(result)
	}
	return intPart + "." + parts[1]
}

// formatCompactMoney formats a float64 as a compact, human-readable string for use in
// summary cards and dashboard widgets where space is at a premium. Values are scaled to
// billions (B), millions (M), or thousands (K) with appropriate decimal precision:
//   - ≥100 in that scale → no decimal (e.g., "150K")
//   - ≥10 and <100 → one decimal (e.g., "15.5K")
//   - <10 → two decimals (e.g., "1.25K")
//
// Values below 1,000 are displayed as-is, with whole numbers shown without decimals
// (e.g., "500") and fractional numbers shown with up to two decimals (e.g., "12.50").
// Negative values are prefixed with a minus sign. The compact display is always
// accompanied by the exact value in a tooltip or data attribute so precision is never lost.
func formatCompactMoney(f float64) string {
	// I compact large values for dense summary cards, while the exact amount is still available
	// through the popover UI driven by the raw numeric value in the template.
	sign := ""
	value := f
	if value < 0 {
		sign = "-"
		value = math.Abs(value)
	}

	// Check thresholds from largest to smallest so that a value of 1,500,000 correctly
	// formats as "1.50M" rather than "1500.00K". Each threshold applies a different
	// precision rule based on how large the compacted value is.
	type threshold struct {
		divisor float64
		suffix  string
	}

	for _, item := range []threshold{
		{divisor: 1_000_000_000, suffix: "B"},
		{divisor: 1_000_000, suffix: "M"},
		{divisor: 1_000, suffix: "K"},
	} {
		if value >= item.divisor {
			compact := value / item.divisor
			if compact >= 100 {
				return fmt.Sprintf("%s%.0f%s", sign, compact, item.suffix)
			}
			if compact >= 10 {
				return fmt.Sprintf("%s%.1f%s", sign, compact, item.suffix)
			}
			return fmt.Sprintf("%s%.2f%s", sign, compact, item.suffix)
		}
	}

	// Values below the smallest threshold (1,000) are displayed without a suffix.
	// Whole numbers omit the decimal portion entirely for cleanliness; fractions
	// keep up to two decimal places.
	if value == math.Trunc(value) {
		return fmt.Sprintf("%s%.0f", sign, value)
	}
	return fmt.Sprintf("%s%.2f", sign, value)
}

// formatMoneyRaw formats a float64 as a minimal numeric string intended for data attributes
// and JavaScript consumption rather than direct display. It always starts with two decimal
// places, then trims trailing zeros and the decimal point if the value is a whole number.
// For example: 1500.00 → "1500", 1500.50 → "1500.5", 1500.05 → "1500.05". Commas are
// deliberately omitted so the output can be parsed as a plain number by JavaScript's
// parseFloat() or used directly in arithmetic operations.
func formatMoneyRaw(f float64) string {
	// I trim trailing zeroes for cases where the raw numeric string is fed into UI helpers rather
	// than being shown as the final formatted accounting amount.
	raw := fmt.Sprintf("%.2f", f)
	raw = strings.TrimRight(raw, "0")
	raw = strings.TrimRight(raw, ".")
	return raw
}

// RenderTemplate parses and executes a page-level template wrapped in the main application
// layout. It combines the shared layout (which includes the sidebar navigation, header,
// and common CSS/JS includes), the partials (reusable template fragments like modals and
// chart containers), and the requested page template into a single template set. The
// layout.html file defines the outer HTML structure with a block placeholder; each page
// template defines the content for that block. Templates are parsed on every request rather
// than cached because the total set of templates is small enough that parsing overhead is
// negligible, and this avoids stale template issues during development.
func RenderTemplate(w http.ResponseWriter, name string, data interface{}) {
	// I parse the page template together with the shared layout and partials on each render so the
	// correct content block wins for the requested page instead of leaking from unrelated templates.
	templates, err := template.New("").Funcs(funcMap).ParseFS(
		templateFS,
		"templates/layout.html",
		"templates/partials/*.html",
		"templates/"+name+".html",
	)
	if err != nil {
		log.Printf("parse template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Execute the specific page template by name (e.g., "dashboard.html"). The layout
	// template acts as the outer shell and invokes the page content via Go's template
	// inheritance mechanism (the {{block}} or {{template}} directive).
	err = templates.ExecuteTemplate(w, name+".html", data)
	if err != nil {
		log.Printf("render template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// RenderStandaloneTemplate parses and executes a single page template without the main
// application layout shell. This is used for pages that exist outside the authenticated
// app frame, such as the login/setup page, which has its own minimal HTML structure and
// should not include the sidebar navigation or other authenticated-only elements. Like
// RenderTemplate, templates are parsed on each request for development convenience.
func RenderStandaloneTemplate(w http.ResponseWriter, name string, data interface{}) {
	// I keep standalone templates separate for screens like login that should not inherit the main
	// authenticated app shell.
	templates, err := template.New("").Funcs(funcMap).ParseFS(
		templateFS,
		"templates/"+name+".html",
	)
	if err != nil {
		log.Printf("parse standalone template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	err = templates.ExecuteTemplate(w, name+".html", data)
	if err != nil {
		log.Printf("render standalone template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
