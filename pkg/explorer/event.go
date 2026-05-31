package explorer

import "time"

type Outcome string

const (
	OutcomeSkipped  Outcome = "skipped"
	OutcomeFiltered Outcome = "filtered"
	OutcomeNoChange Outcome = "no_change"
	OutcomeActed    Outcome = "acted"
	OutcomeErrored  Outcome = "errored"
)

type LogLine struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]any
}

type Event struct {
	Time      time.Time
	EventGUID string
	EventType string
	Org       string
	Repo      string
	PR        int
	Action    string
	Actor     string
	URL       string
	Outcome   Outcome
	Reason    string
	Duration  time.Duration
	Logs      []LogLine
}
