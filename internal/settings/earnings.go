package settings

import (
	_ "embed"
	"encoding/json"
	"time"
)

//go:embed holidays/holidays-2026.json
var holidaysJSON []byte

type holidayData struct {
	Holidays []string `json:"holidays"`
	Workdays []string `json:"workdays"`
}

var loadedHolidays *holidayData

func getHolidayData() *holidayData {
	if loadedHolidays != nil {
		return loadedHolidays
	}
	var h holidayData
	if err := json.Unmarshal(holidaysJSON, &h); err != nil {
		loadedHolidays = &holidayData{}
		return loadedHolidays
	}
	loadedHolidays = &h
	return loadedHolidays
}

const (
	workStartSec      = 9 * 3600  // 09:00
	lunchStartSec     = 12 * 3600 // 12:00
	lunchEndSec       = 13 * 3600 // 13:00
	workEndSec        = 18 * 3600 // 18:00
	workSecondsPerDay = 28800     // 8 hours = 28800 seconds
	workDaysPerMonth  = 22
)

// EarningsStatus describes the current earning state.
type EarningsStatus string

const (
	EarningsBeforeWork EarningsStatus = "before-work"
	EarningsWorking    EarningsStatus = "working"
	EarningsLunchBreak EarningsStatus = "lunch-break"
	EarningsAfterWork  EarningsStatus = "after-work"
	EarningsRestDay    EarningsStatus = "rest-day"
)

// CalculateEarnedToday returns the amount earned so far today in CNY.
// Returns 0 if monthlySalary is not configured or today is a rest day.
func CalculateEarnedToday(monthlySalary float64) (float64, EarningsStatus) {
	if monthlySalary <= 0 {
		return 0, EarningsRestDay
	}

	now := time.Now()

	if !isWorkday(now) {
		return 0, EarningsRestDay
	}

	dailySalary := monthlySalary / workDaysPerMonth

	nowSec := now.Hour()*3600 + now.Minute()*60 + now.Second()

	switch {
	case nowSec < workStartSec:
		return 0, EarningsBeforeWork
	case nowSec < lunchStartSec:
		worked := nowSec - workStartSec
		return dailySalary * float64(worked) / workSecondsPerDay, EarningsWorking
	case nowSec < lunchEndSec:
		worked := lunchStartSec - workStartSec
		return dailySalary * float64(worked) / workSecondsPerDay, EarningsLunchBreak
	case nowSec < workEndSec:
		worked := (lunchStartSec - workStartSec) + (nowSec - lunchEndSec)
		return dailySalary * float64(worked) / workSecondsPerDay, EarningsWorking
	default:
		return dailySalary, EarningsAfterWork
	}
}

// isWorkday returns true if the given time falls on a workday
// (not a weekend or holiday, or a makeup workday).
func isWorkday(t time.Time) bool {
	h := getHolidayData()
	dateStr := t.Format("2006-01-02")

	// Check makeup workdays first (调休上班日 override weekends)
	for _, wd := range h.Workdays {
		if wd == dateStr {
			return true
		}
	}

	// Weekends are rest days
	weekday := t.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	// Check holidays
	for _, hd := range h.Holidays {
		if hd == dateStr {
			return false
		}
	}

	return true
}
