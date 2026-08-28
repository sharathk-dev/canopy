package scheduler

import (
	"testing"
	"time"
)

func TestCronMatches(t *testing.T) {
	cron, err := ParseCron("0 9 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	if !cron.Matches(time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("weekday at 09:00 should match")
	}
	if cron.Matches(time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("Saturday should not match")
	}
}

func TestCronRejectsInvalidExpression(t *testing.T) {
	if _, err := ParseCron("0 9 * *"); err == nil {
		t.Fatal("expected invalid cron expression")
	}
	if _, err := ParseCron("61 9 * * *"); err == nil {
		t.Fatal("expected out-of-range minute")
	}
}

func TestCronUsesEitherRestrictedDayField(t *testing.T) {
	cron, err := ParseCron("0 9 15 * 1")
	if err != nil {
		t.Fatal(err)
	}
	if !cron.Matches(time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("expected day-of-month match")
	}
	if !cron.Matches(time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("expected day-of-week match")
	}
}
