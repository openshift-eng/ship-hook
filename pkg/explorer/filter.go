package explorer

import "time"

type ShowLevel int

const (
	ShowActed ShowLevel = iota
	ShowDomain
	ShowAll
)

func ParseShowLevel(s string) (ShowLevel, bool) {
	switch s {
	case "acted":
		return ShowActed, true
	case "domain":
		return ShowDomain, true
	case "all":
		return ShowAll, true
	default:
		return 0, false
	}
}

func ShouldShow(outcome Outcome, level ShowLevel) bool {
	if outcome == OutcomeErrored {
		return true
	}
	switch level {
	case ShowAll:
		return true
	case ShowDomain:
		return outcome == OutcomeActed || outcome == OutcomeNoChange
	case ShowActed:
		return outcome == OutcomeActed
	default:
		return false
	}
}

type Filter struct {
	Org   string
	Repo  string
	PR    int
	Since time.Time
	Until time.Time
}

func (f *Filter) Matches(ev *Event) bool {
	if f.Org != "" && ev.Org != f.Org {
		return false
	}
	if f.Repo != "" && ev.Repo != f.Repo {
		return false
	}
	if f.PR != 0 && ev.PR != f.PR {
		return false
	}
	if !f.Since.IsZero() && ev.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Time.After(f.Until) {
		return false
	}
	return true
}
