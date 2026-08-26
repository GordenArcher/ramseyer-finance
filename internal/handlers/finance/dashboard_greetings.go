package finance

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"ramseyer-finance/internal/db"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	dashboardDisplayNameSettingKey     = "dashboard_display_name"
	dashboardGreetingTimeSettingKey    = "dashboard_greeting_rotation_time"
	dashboardGreetingStateSettingKey   = "dashboard_greeting_cycle_state"
	dashboardGreetingEnabledSettingKey = "dashboard_greeting_enabled"
	defaultGreetingRotationTime        = "10:00"
)

type dashboardGreeting struct {
	ID      int
	Message string
}

type dashboardGreetingState struct {
	Order        []int  `json:"order"`
	CurrentIndex int    `json:"current_index"`
	LastSlot     string `json:"last_slot"`
}

type DashboardGreetingConfig struct {
	Enabled              bool
	DisplayName          string
	RotationTime         string
	RotationTimeLabel    string
	GreetingCount        int
	CurrentGreeting      string
	CurrentGreetingIntro string
	ShowSetupHint        bool
}

// loadDashboardGreetingState resolves the operator-facing name and the non-repeating greeting
// for the current dashboard view. I keep the rotation server-side so the same greeting appears
// no matter which page or shell opens the dashboard, and so the "already used" sequence survives
// restarts instead of resetting in browser memory.
func loadDashboardGreetingState(now time.Time) (DashboardGreetingConfig, error) {
	enabledValue, err := db.GetSetting(dashboardGreetingEnabledSettingKey)
	if err != nil {
		return DashboardGreetingConfig{}, fmt.Errorf("load dashboard greeting enabled flag: %w", err)
	}
	enabled := enabledValue != "0"

	displayName, err := db.GetSetting(dashboardDisplayNameSettingKey)
	if err != nil {
		return DashboardGreetingConfig{}, fmt.Errorf("load dashboard display name: %w", err)
	}

	rotationTime, err := db.GetSetting(dashboardGreetingTimeSettingKey)
	if err != nil {
		return DashboardGreetingConfig{}, fmt.Errorf("load dashboard greeting rotation time: %w", err)
	}
	if !isValidGreetingRotationTime(rotationTime) {
		rotationTime = defaultGreetingRotationTime
	}

	greetings, err := loadActiveDashboardGreetings()
	if err != nil {
		return DashboardGreetingConfig{}, err
	}
	if len(greetings) == 0 {
		return DashboardGreetingConfig{
			Enabled:           enabled,
			DisplayName:       displayName,
			RotationTime:      rotationTime,
			RotationTimeLabel: formatGreetingRotationTime(rotationTime),
			ShowSetupHint:     enabled && strings.TrimSpace(displayName) == "",
		}, nil
	}

	if !enabled {
		return DashboardGreetingConfig{
			Enabled:           false,
			DisplayName:       displayName,
			RotationTime:      rotationTime,
			RotationTimeLabel: formatGreetingRotationTime(rotationTime),
			GreetingCount:     len(greetings),
			ShowSetupHint:     false,
		}, nil
	}

	currentGreeting, err := resolveCurrentDashboardGreeting(now, rotationTime, greetings)
	if err != nil {
		return DashboardGreetingConfig{}, err
	}

	return DashboardGreetingConfig{
		Enabled:              true,
		DisplayName:          displayName,
		RotationTime:         rotationTime,
		RotationTimeLabel:    formatGreetingRotationTime(rotationTime),
		GreetingCount:        len(greetings),
		CurrentGreeting:      currentGreeting.Message,
		CurrentGreetingIntro: buildGreetingIntro(now, strings.TrimSpace(displayName)),
		ShowSetupHint:        strings.TrimSpace(displayName) == "",
	}, nil
}

func loadActiveDashboardGreetings() ([]dashboardGreeting, error) {
	// I read only active greetings here so the rotation can support future pruning or customization
	// without having to change the rotation algorithm itself.
	rows, err := db.DB.Query(`
		SELECT id, message
		FROM dashboard_greetings
		WHERE COALESCE(is_active, 1) = 1
		ORDER BY sort_order, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query dashboard greetings: %w", err)
	}
	defer rows.Close()

	var greetings []dashboardGreeting
	for rows.Next() {
		var item dashboardGreeting
		if err := rows.Scan(&item.ID, &item.Message); err != nil {
			return nil, fmt.Errorf("scan dashboard greeting: %w", err)
		}
		greetings = append(greetings, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard greetings: %w", err)
	}
	return greetings, nil
}

func resolveCurrentDashboardGreeting(now time.Time, rotationTime string, greetings []dashboardGreeting) (dashboardGreeting, error) {
	slotTime, err := resolveGreetingSlotTime(now, rotationTime)
	if err != nil {
		return dashboardGreeting{}, err
	}
	slotKey := slotTime.Format("2006-01-02")

	state, changed, err := loadOrInitializeDashboardGreetingState(greetings, slotKey)
	if err != nil {
		return dashboardGreeting{}, err
	}

	if state.LastSlot != slotKey {
		daysElapsed := daysBetweenSlotKeys(state.LastSlot, slotKey)
		if daysElapsed > 0 {
			for step := 0; step < daysElapsed; step++ {
				advanceDashboardGreetingState(&state, greetings)
			}
			state.LastSlot = slotKey
			changed = true
		}
	}

	currentID := state.Order[state.CurrentIndex]
	currentGreeting, ok := findDashboardGreetingByID(greetings, currentID)
	if !ok {
		state = newDashboardGreetingState(greetings, slotKey)
		currentGreeting = greetingsByID(greetings)[state.Order[state.CurrentIndex]]
		changed = true
	}

	if changed {
		if err := saveDashboardGreetingState(state); err != nil {
			return dashboardGreeting{}, err
		}
	}

	return currentGreeting, nil
}

func loadOrInitializeDashboardGreetingState(greetings []dashboardGreeting, slotKey string) (dashboardGreetingState, bool, error) {
	rawState, err := db.GetSetting(dashboardGreetingStateSettingKey)
	if err != nil {
		return dashboardGreetingState{}, false, fmt.Errorf("load dashboard greeting state: %w", err)
	}

	if strings.TrimSpace(rawState) == "" {
		return newDashboardGreetingState(greetings, slotKey), true, nil
	}

	var state dashboardGreetingState
	if err := json.Unmarshal([]byte(rawState), &state); err != nil {
		return newDashboardGreetingState(greetings, slotKey), true, nil
	}

	if !dashboardGreetingStateMatches(state, greetings) {
		// I rebuild the cycle if the active greeting set changed because a stale order would either
		// point at removed ids or silently exclude new ones from the rotation forever.
		return newDashboardGreetingState(greetings, slotKey), true, nil
	}

	if state.CurrentIndex < 0 || state.CurrentIndex >= len(state.Order) {
		return newDashboardGreetingState(greetings, slotKey), true, nil
	}

	return state, false, nil
}

func newDashboardGreetingState(greetings []dashboardGreeting, slotKey string) dashboardGreetingState {
	order := shuffledGreetingIDs(greetings)
	return dashboardGreetingState{
		Order:        order,
		CurrentIndex: 0,
		LastSlot:     slotKey,
	}
}

func dashboardGreetingStateMatches(state dashboardGreetingState, greetings []dashboardGreeting) bool {
	if len(state.Order) != len(greetings) || len(state.Order) == 0 {
		return false
	}

	activeIDs := make([]int, 0, len(greetings))
	for _, greeting := range greetings {
		activeIDs = append(activeIDs, greeting.ID)
	}
	sort.Ints(activeIDs)

	orderCopy := append([]int(nil), state.Order...)
	sort.Ints(orderCopy)
	for index := range orderCopy {
		if orderCopy[index] != activeIDs[index] {
			return false
		}
	}
	return true
}

func advanceDashboardGreetingState(state *dashboardGreetingState, greetings []dashboardGreeting) {
	if len(state.Order) == 0 {
		*state = newDashboardGreetingState(greetings, state.LastSlot)
		return
	}

	if state.CurrentIndex < len(state.Order)-1 {
		state.CurrentIndex++
		return
	}

	lastGreetingID := state.Order[state.CurrentIndex]
	state.Order = shuffledGreetingIDs(greetings)
	if len(state.Order) > 1 && state.Order[0] == lastGreetingID {
		state.Order[0], state.Order[1] = state.Order[1], state.Order[0]
	}
	state.CurrentIndex = 0
}

func shuffledGreetingIDs(greetings []dashboardGreeting) []int {
	ids := make([]int, 0, len(greetings))
	for _, greeting := range greetings {
		ids = append(ids, greeting.ID)
	}

	// I persist the shuffled order after generating it, so this randomization happens only when a
	// new cycle is created. Day-to-day reads stay deterministic because they consume the saved order.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(ids), func(i, j int) {
		ids[i], ids[j] = ids[j], ids[i]
	})
	return ids
}

func saveDashboardGreetingState(state dashboardGreetingState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal dashboard greeting state: %w", err)
	}
	if err := db.SetSetting(dashboardGreetingStateSettingKey, string(payload)); err != nil {
		return fmt.Errorf("save dashboard greeting state: %w", err)
	}
	return nil
}

func resolveGreetingSlotTime(now time.Time, rotationTime string) (time.Time, error) {
	hour, minute, err := parseGreetingRotationTime(rotationTime)
	if err != nil {
		return time.Time{}, err
	}

	cutoff := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if now.Before(cutoff) {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	return cutoff, nil
}

func parseGreetingRotationTime(value string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid greeting rotation time")
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid greeting rotation hour")
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid greeting rotation minute")
	}

	return hour, minute, nil
}

func formatGreetingRotationTime(value string) string {
	hour, minute, err := parseGreetingRotationTime(value)
	if err != nil {
		hour, minute = 10, 0
	}
	display := time.Date(2000, time.January, 1, hour, minute, 0, 0, time.Local)
	return display.Format("3:04 PM")
}

func isValidGreetingRotationTime(value string) bool {
	_, _, err := parseGreetingRotationTime(value)
	return err == nil
}

func daysBetweenSlotKeys(previous, current string) int {
	previousDate, err := time.Parse("2006-01-02", previous)
	if err != nil {
		return 0
	}
	currentDate, err := time.Parse("2006-01-02", current)
	if err != nil {
		return 0
	}
	if !currentDate.After(previousDate) {
		return 0
	}
	return int(currentDate.Sub(previousDate).Hours() / 24)
}

func findDashboardGreetingByID(greetings []dashboardGreeting, id int) (dashboardGreeting, bool) {
	for _, greeting := range greetings {
		if greeting.ID == id {
			return greeting, true
		}
	}
	return dashboardGreeting{}, false
}

func greetingsByID(greetings []dashboardGreeting) map[int]dashboardGreeting {
	index := make(map[int]dashboardGreeting, len(greetings))
	for _, greeting := range greetings {
		index[greeting.ID] = greeting
	}
	return index
}

func buildGreetingIntro(now time.Time, displayName string) string {
	// I deliberately return no salutation when the operator has not configured a name yet.
	// A bare "Good morning." reads awkwardly on its own, and when the user is expecting a more
	// personal dashboard it feels like filler rather than an intentional greeting.
	if displayName == "" {
		return ""
	}

	hour := now.Hour()
	intro := "Welcome"
	switch {
	case hour < 12:
		intro = "Good morning"
	case hour < 17:
		intro = "Good afternoon"
	default:
		intro = "Good evening"
	}

	return intro + ", " + displayName + "."
}
