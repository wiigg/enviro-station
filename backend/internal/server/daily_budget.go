package server

import (
	"sync"
	"time"
)

type dailyRequestBudget struct {
	mu     sync.Mutex
	limit  int
	day    int64
	used   int
	logged bool
}

func newDailyRequestBudget(limit int) *dailyRequestBudget {
	return &dailyRequestBudget{limit: limit}
}

func (budget *dailyRequestBudget) take(now time.Time) bool {
	if budget == nil || budget.limit <= 0 {
		return true
	}

	day := now.UTC().Truncate(24 * time.Hour).Unix()
	budget.mu.Lock()
	defer budget.mu.Unlock()

	if budget.day != day {
		budget.day = day
		budget.used = 0
		budget.logged = false
	}
	if budget.used >= budget.limit {
		return false
	}
	budget.used++
	return true
}

func (budget *dailyRequestBudget) markExhaustionLogged() bool {
	if budget == nil {
		return false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.logged {
		return false
	}
	budget.logged = true
	return true
}
