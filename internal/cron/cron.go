// Package cron parses five-field cron expressions (minute hour
// day-of-month month day-of-week) and matches them against minutes in a
// time zone. It is what the workflow `schedule` trigger runs on.
//
// Supported syntax per field: `*`, numbers, ranges `a-b`, steps `*/n` and
// `a-b/n` (and `a/n`, meaning `a-max/n`), and comma-separated lists of
// these. Months accept JAN–DEC and days of the week SUN–SAT
// (case-insensitive); day-of-week 0 and 7 are both Sunday. As in classic
// cron, when both day-of-month and day-of-week are restricted (neither is
// `*`), a day matches when either matches.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	// Schedules name IANA zones; embed the database so matching does not
	// depend on the host having zoneinfo installed (Windows, scratch images).
	_ "time/tzdata"
)

// Schedule is a parsed expression bound to a location.
type Schedule struct {
	minute, hour, dom, month, dow uint64
	domStar, dowStar              bool
	location                      *time.Location
}

type field struct {
	name     string
	min, max int
	names    map[string]int
}

var (
	minuteField = field{name: "minute", min: 0, max: 59}
	hourField   = field{name: "hour", min: 0, max: 23}
	domField    = field{name: "day of month", min: 1, max: 31}
	monthField  = field{name: "month", min: 1, max: 12, names: map[string]int{
		"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
		"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
	}}
	dowField = field{name: "day of week", min: 0, max: 7, names: map[string]int{
		"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
	}}
)

// Parse parses expression in the IANA time zone timezone ("" means UTC).
func Parse(expression, timezone string) (Schedule, error) {
	location := time.UTC
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return Schedule{}, fmt.Errorf("unknown timezone %q", timezone)
		}
		location = loaded
	}
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return Schedule{}, fmt.Errorf("cron expression %q must have 5 fields (minute hour day-of-month month day-of-week), got %d", expression, len(parts))
	}
	schedule := Schedule{location: location}
	var err error
	if schedule.minute, err = minuteField.parse(parts[0]); err != nil {
		return Schedule{}, err
	}
	if schedule.hour, err = hourField.parse(parts[1]); err != nil {
		return Schedule{}, err
	}
	if schedule.dom, err = domField.parse(parts[2]); err != nil {
		return Schedule{}, err
	}
	if schedule.month, err = monthField.parse(parts[3]); err != nil {
		return Schedule{}, err
	}
	if schedule.dow, err = dowField.parse(parts[4]); err != nil {
		return Schedule{}, err
	}
	if schedule.dow&(1<<7) != 0 {
		schedule.dow |= 1
		schedule.dow &^= 1 << 7
	}
	schedule.domStar = isStar(parts[2])
	schedule.dowStar = isStar(parts[4])
	return schedule, nil
}

func isStar(text string) bool {
	return text == "*" || strings.HasPrefix(text, "*/")
}

// Matches reports whether the minute containing at (in the schedule's
// time zone) is one the expression names.
func (schedule Schedule) Matches(at time.Time) bool {
	local := at.In(schedule.location)
	if schedule.minute&(1<<local.Minute()) == 0 || schedule.hour&(1<<local.Hour()) == 0 || schedule.month&(1<<int(local.Month())) == 0 {
		return false
	}
	domMatch := schedule.dom&(1<<local.Day()) != 0
	dowMatch := schedule.dow&(1<<int(local.Weekday())) != 0
	switch {
	case schedule.domStar && schedule.dowStar:
		return true
	case schedule.domStar:
		return dowMatch
	case schedule.dowStar:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}

// Next returns the first matching minute strictly after after, searching
// up to five years ahead; ok is false when there is none (e.g. 30 FEB).
func (schedule Schedule) Next(after time.Time) (time.Time, bool) {
	at := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(5, 0, 0)
	for at.Before(limit) {
		if schedule.Matches(at) {
			return at, true
		}
		at = at.Add(time.Minute)
	}
	return time.Time{}, false
}

func (f field) parse(text string) (uint64, error) {
	var bits uint64
	for _, item := range strings.Split(text, ",") {
		itemBits, err := f.parseItem(item)
		if err != nil {
			return 0, err
		}
		bits |= itemBits
	}
	return bits, nil
}

func (f field) parseItem(item string) (uint64, error) {
	if item == "" {
		return 0, fmt.Errorf("cron %s has an empty list item", f.name)
	}
	rangeText, stepText, hasStep := strings.Cut(item, "/")
	step := 1
	if hasStep {
		parsed, err := strconv.Atoi(stepText)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("cron %s step %q must be a positive number", f.name, stepText)
		}
		step = parsed
	}
	low, high := f.min, f.max
	if f.name == dowField.name && rangeText == "*" {
		high = 6
	}
	if rangeText != "*" {
		lowText, highText, isRange := strings.Cut(rangeText, "-")
		var err error
		if low, err = f.value(lowText); err != nil {
			return 0, err
		}
		switch {
		case isRange:
			if high, err = f.value(highText); err != nil {
				return 0, err
			}
			if high < low {
				return 0, fmt.Errorf("cron %s range %q runs backwards", f.name, rangeText)
			}
		case hasStep:
			// "a/n" means from a to the end of the range.
		default:
			high = low
		}
	}
	var bits uint64
	for value := low; value <= high; value += step {
		bits |= 1 << value
	}
	return bits, nil
}

func (f field) value(text string) (int, error) {
	if value, ok := f.names[strings.ToUpper(text)]; ok {
		return value, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("cron %s value %q is not a number", f.name, text)
	}
	if value < f.min || value > f.max {
		return 0, fmt.Errorf("cron %s value %d is outside %d-%d", f.name, value, f.min, f.max)
	}
	return value, nil
}
