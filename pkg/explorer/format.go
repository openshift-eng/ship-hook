package explorer

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

var outcomeIndicator = map[Outcome]string{
	OutcomeActed:    "✅",
	OutcomeNoChange: "💤",
	OutcomeFiltered: "🔽",
	OutcomeSkipped:  "⬜",
	OutcomeErrored:  "❌",
}

var shortEventType = map[string]string{
	"pull_request":        "pr",
	"pull_request_review": "review",
	"issue_comment":       "comment",
}

func FormatEvent(ev *Event) string {
	indicator := outcomeIndicator[ev.Outcome]
	if indicator == "" {
		indicator = "?"
	}

	eventType := shortEventType[ev.EventType]
	if eventType == "" {
		eventType = ev.EventType
	}

	ref := fmt.Sprintf("%s/%s#%d", ev.Org, ev.Repo, ev.PR)

	reason := ev.Reason
	if ev.Outcome == OutcomeActed && ev.Duration > 0 {
		reason = fmt.Sprintf("%s (%s)", ev.Reason, ev.Duration)
	}
	if ev.Outcome == OutcomeErrored && ev.Duration > 0 {
		reason = fmt.Sprintf("%s (%s)", ev.Reason, ev.Duration)
	}

	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s %s",
		ev.Time.Format("2006-01-02 15:04:05"),
		eventType,
		ref,
		ev.Action,
		ev.Actor,
		indicator,
		reason,
	)
}

func FormatLogLine(ll *LogLine) string {
	msg := ll.Message
	if len(ll.Fields) > 0 {
		var parts []string
		for k, v := range ll.Fields {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
		sort.Strings(parts)
		msg = fmt.Sprintf("%s (%s)", msg, strings.Join(parts, ", "))
	}
	return fmt.Sprintf("                     [%-5s] %s", ll.Level, msg)
}

type jsonLogLine struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

func FormatEventJSON(w io.Writer, ev *Event, verbose bool) error {
	out := struct {
		Time      string        `json:"time"`
		EventType string        `json:"event_type"`
		Org       string        `json:"org"`
		Repo      string        `json:"repo"`
		PR        int           `json:"pr"`
		Action    string        `json:"action"`
		Actor     string        `json:"actor"`
		Outcome   string        `json:"outcome"`
		Reason    string        `json:"reason"`
		Duration  string        `json:"duration,omitempty"`
		URL       string        `json:"url,omitempty"`
		EventGUID string        `json:"event_guid"`
		Logs      []jsonLogLine `json:"logs,omitempty"`
	}{
		Time:      ev.Time.Format(time.RFC3339),
		EventType: ev.EventType,
		Org:       ev.Org,
		Repo:      ev.Repo,
		PR:        ev.PR,
		Action:    ev.Action,
		Actor:     ev.Actor,
		Outcome:   string(ev.Outcome),
		Reason:    ev.Reason,
		URL:       ev.URL,
		EventGUID: ev.EventGUID,
	}
	if ev.Duration > 0 {
		out.Duration = ev.Duration.String()
	}
	if verbose && len(ev.Logs) > 0 {
		for _, ll := range ev.Logs {
			out.Logs = append(out.Logs, jsonLogLine{
				Time:    ll.Time.Format(time.RFC3339),
				Level:   ll.Level,
				Message: ll.Message,
				Fields:  ll.Fields,
			})
		}
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

type Summary struct {
	Plugin   string
	Total    int
	Shown    int
	Acted    int
	Errored  int
	NoChange int
	Filtered int
	Skipped  int
}

func (s *Summary) Add(outcome Outcome) {
	s.Total++
	switch outcome {
	case OutcomeActed:
		s.Acted++
	case OutcomeErrored:
		s.Errored++
	case OutcomeNoChange:
		s.NoChange++
	case OutcomeFiltered:
		s.Filtered++
	case OutcomeSkipped:
		s.Skipped++
	}
}

func (s *Summary) String() string {
	header := fmt.Sprintf("%d events", s.Total)
	if s.Shown != s.Total {
		header = fmt.Sprintf("%d events (%d shown)", s.Total, s.Shown)
	}
	parts := []string{header}
	if s.Acted > 0 {
		parts = append(parts, fmt.Sprintf("%d acted", s.Acted))
	}
	if s.Errored > 0 {
		parts = append(parts, fmt.Sprintf("%d errored", s.Errored))
	}
	if s.NoChange > 0 {
		parts = append(parts, fmt.Sprintf("%d no_change", s.NoChange))
	}
	if s.Filtered > 0 {
		parts = append(parts, fmt.Sprintf("%d filtered", s.Filtered))
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	return fmt.Sprintf("--- %s: %s ---", s.Plugin, strings.Join(parts, " | "))
}
