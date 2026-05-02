package db

import "fmt"

// seedDashboardGreetings ensures the dashboard has a full pool of default greetings to rotate
// through. I seed only when the table is empty because once a deployment has live greeting rows,
// the database should remain the source of truth rather than being overwritten at each startup.
func seedDashboardGreetings() error {
	var count int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM dashboard_greetings`).Scan(&count); err != nil {
		return fmt.Errorf("count dashboard greetings: %w", err)
	}
	if count > 0 {
		return nil
	}

	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("begin dashboard greetings seed: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO dashboard_greetings (message, is_active, sort_order)
		VALUES (?, 1, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare dashboard greetings seed: %w", err)
	}
	defer stmt.Close()

	for index, message := range defaultDashboardGreetings() {
		if _, err := stmt.Exec(message, index+1); err != nil {
			return fmt.Errorf("insert dashboard greeting %d: %w", index+1, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit dashboard greetings seed: %w", err)
	}
	return nil
}

// defaultDashboardGreetings returns 200 seeded greeting lines built from a curated pair of
// phrase banks. I generate them in code instead of hardcoding 200 manual INSERT statements so
// the seed stays readable, easy to audit, and still produces a large non-repeating pool.
func defaultDashboardGreetings() []string {
	intros := []string{
		"Grace goes before you today",
		"Peace is already waiting for you this morning",
		"Strength has been prepared for today's work",
		"Mercy is fresh for the path ahead",
		"Today's labor can still be carried with joy",
		"Faithfulness is quietly holding today's plans together",
		"Hope is still brighter than the pressure of the day",
		"Wisdom can meet every decision before you",
		"Courage belongs in today's responsibilities",
		"Favor can still be found in ordinary duties",
		"Rest can shape the way today's work is carried",
		"Kindness can lead every conversation today",
		"Steadiness can govern the work in front of you",
		"Calm can stay with you through every task today",
		"Clarity can guide each step of this day",
		"Patience can hold today's work in order",
		"Fresh strength can rise with today's assignment",
		"Goodness can follow you through today's schedule",
		"Light can reach every responsibility in front of you",
		"Purpose can stay clear throughout today's work",
	}

	closings := []string{
		"and may your hands find fruitful work.",
		"and may every task be carried with wisdom.",
		"and may today's effort return with peace.",
		"and may your decisions stay settled and clear.",
		"and may quiet joy remain with you all day.",
		"and may your service be steady and fruitful.",
		"and may each responsibility meet enough strength.",
		"and may your steps stay ordered and confident.",
		"and may the day unfold with unusual calm.",
		"and may what you build today be filled with grace.",
	}

	greetings := make([]string, 0, len(intros)*len(closings))
	for _, intro := range intros {
		for _, closing := range closings {
			greetings = append(greetings, intro+", "+closing)
		}
	}
	return greetings
}
