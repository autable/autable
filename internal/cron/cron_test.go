package cron

import (
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, expression, timezone string) Schedule {
	t.Helper()
	schedule, err := Parse(expression, timezone)
	if err != nil {
		t.Fatalf("Parse(%q, %q): %v", expression, timezone, err)
	}
	return schedule
}

func utc(text string) time.Time {
	at, err := time.Parse("2006-01-02 15:04", text)
	if err != nil {
		panic(err)
	}
	return at
}

func TestMatches(t *testing.T) {
	cases := []struct {
		expression string
		at         string
		want       bool
	}{
		{"* * * * *", "2026-03-04 05:06", true},
		{"0 9 * * 1-5", "2026-11-20 09:00", true},  // Friday
		{"0 9 * * 1-5", "2026-11-21 09:00", false}, // Saturday
		{"0 9 * * 1-5", "2026-11-20 09:01", false},
		{"*/15 * * * *", "2026-11-20 10:45", true},
		{"*/15 * * * *", "2026-11-20 10:50", false},
		{"5/20 * * * *", "2026-11-20 10:45", true},
		{"5/20 * * * *", "2026-11-20 10:35", false},
		{"0 0 1,15 * *", "2026-11-15 00:00", true},
		{"0 0 * JAN,feb *", "2026-02-10 00:00", true},
		{"0 0 * JAN,feb *", "2026-03-10 00:00", false},
		{"0 12 * * SUN", "2026-11-22 12:00", true},
		{"0 12 * * 7", "2026-11-22 12:00", true},
		{"0 12 * * 0", "2026-11-22 12:00", true},
		{"0 12 * * 5-7", "2026-11-22 12:00", true},
		// Both day fields restricted: either matches.
		{"0 0 13 * FRI", "2026-11-13 00:00", true},
		{"0 0 13 * FRI", "2026-11-20 00:00", true},
		{"0 0 13 * FRI", "2026-11-19 00:00", false},
		// Day of week as */n is still "unrestricted" for the OR rule.
		{"0 0 1 * */2", "2026-11-02 00:00", false},
	}
	for _, tc := range cases {
		got := mustParse(t, tc.expression, "").Matches(utc(tc.at))
		if got != tc.want {
			t.Errorf("%q at %s: got %v, want %v", tc.expression, tc.at, got, tc.want)
		}
	}
}

func TestTimezone(t *testing.T) {
	schedule := mustParse(t, "0 9 * * *", "Asia/Shanghai")
	if !schedule.Matches(utc("2026-11-20 01:00")) {
		t.Fatal("expected 09:00 Shanghai to be 01:00 UTC")
	}
	if schedule.Matches(utc("2026-11-20 09:00")) {
		t.Fatal("did not expect 09:00 UTC to match")
	}
	// Daylight saving: 09:00 New York is 13:00 UTC in summer, 14:00 in winter.
	ny := mustParse(t, "0 9 * * *", "America/New_York")
	if !ny.Matches(utc("2026-07-01 13:00")) || !ny.Matches(utc("2026-12-01 14:00")) || ny.Matches(utc("2026-12-01 13:00")) {
		t.Fatal("expected New York matching to follow daylight saving")
	}
	next, ok := schedule.Next(utc("2026-11-20 01:00"))
	if !ok || !next.Equal(utc("2026-11-21 01:00")) {
		t.Fatalf("unexpected next run %v %v", next, ok)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"* * * *":       "5 fields",
		"60 * * * *":    "outside 0-59",
		"* 24 * * *":    "outside 0-23",
		"* * 0 * *":     "outside 1-31",
		"* * * 13 *":    "outside 1-12",
		"* * * * 8":     "outside 0-7",
		"*/0 * * * *":   "positive number",
		"5-1 * * * *":   "backwards",
		"a * * * *":     "not a number",
		"1,,2 * * * *":  "empty list item",
		"* * * * MON-X": "not a number",
	}
	for expression, want := range cases {
		if _, err := Parse(expression, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q): expected error containing %q, got %v", expression, want, err)
		}
	}
	if _, err := Parse("* * * * *", "Mars/Olympus"); err == nil || !strings.Contains(err.Error(), "unknown timezone") {
		t.Fatalf("expected an unknown timezone error, got %v", err)
	}
	if _, ok := mustParse(t, "0 0 30 2 *", "").Next(utc("2026-01-01 00:00")); ok {
		t.Fatal("expected 30 February never to match")
	}
}
