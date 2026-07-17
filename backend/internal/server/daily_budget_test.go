package server

import (
	"testing"
	"time"
)

func TestDailyRequestBudgetCapsAndResetsByUTCDay(t *testing.T) {
	budget := newDailyRequestBudget(2)
	dayOne := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)

	if !budget.take(dayOne) || !budget.take(dayOne.Add(time.Hour)) {
		t.Fatal("expected requests within the daily limit")
	}
	if budget.take(dayOne.Add(2 * time.Hour)) {
		t.Fatal("expected request above the daily limit to be rejected")
	}
	if !budget.markExhaustionLogged() || budget.markExhaustionLogged() {
		t.Fatal("expected exhaustion to be reported once per UTC day")
	}
	if !budget.take(dayOne.Add(24 * time.Hour)) {
		t.Fatal("expected budget to reset on the next UTC day")
	}
}
