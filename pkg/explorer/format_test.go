package explorer

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatEvent(t *testing.T) {
	ev := &Event{
		Time:      time.Date(2026, 5, 28, 20, 47, 37, 0, time.UTC),
		EventType: "pull_request_review",
		Org:       "openshift",
		Repo:      "sippy",
		PR:        3557,
		Action:    "submitted",
		Actor:     "coderabbitai[bot]",
		Outcome:   OutcomeActed,
		Reason:    "added ready-for-humans label",
		Duration:  3200 * time.Microsecond,
	}

	got := FormatEvent(ev)

	if !strings.Contains(got, "2026-05-28 20:47:37") {
		t.Errorf("missing timestamp in %q", got)
	}
	if !strings.Contains(got, "review") {
		t.Errorf("missing event type in %q", got)
	}
	if !strings.Contains(got, "openshift/sippy#3557") {
		t.Errorf("missing ref in %q", got)
	}
	if !strings.Contains(got, "✅") {
		t.Errorf("missing acted indicator in %q", got)
	}
	if !strings.Contains(got, "added ready-for-humans label") {
		t.Errorf("missing reason in %q", got)
	}
	if !strings.Contains(got, "coderabbitai[bot]") {
		t.Errorf("missing actor in %q", got)
	}
}

func TestFormatEventJSON(t *testing.T) {
	ev := &Event{
		Time:      time.Date(2026, 5, 28, 20, 47, 37, 0, time.UTC),
		EventGUID: "abc-123",
		EventType: "pull_request_review",
		Org:       "openshift",
		Repo:      "sippy",
		PR:        3557,
		Action:    "submitted",
		Actor:     "coderabbitai[bot]",
		URL:       "https://github.com/openshift/sippy/pull/3557",
		Outcome:   OutcomeActed,
		Reason:    "added ready-for-humans label",
		Duration:  3200 * time.Microsecond,
	}

	var buf bytes.Buffer
	if err := FormatEventJSON(&buf, ev, false); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, `"outcome":"acted"`) {
		t.Errorf("missing outcome in %q", got)
	}
	if !strings.Contains(got, `"event_guid":"abc-123"`) {
		t.Errorf("missing event_guid in %q", got)
	}
}

func TestSummary(t *testing.T) {
	s := &Summary{Plugin: "ready-for-humans"}
	s.Add(OutcomeActed)
	s.Add(OutcomeActed)
	s.Add(OutcomeErrored)
	s.Add(OutcomeNoChange)
	s.Add(OutcomeFiltered)
	s.Add(OutcomeFiltered)
	s.Add(OutcomeSkipped)

	got := s.String()
	if !strings.Contains(got, "7 events") {
		t.Errorf("wrong total in %q", got)
	}
	if !strings.Contains(got, "2 acted") {
		t.Errorf("wrong acted count in %q", got)
	}
	if !strings.Contains(got, "1 errored") {
		t.Errorf("wrong errored count in %q", got)
	}
	if !strings.Contains(got, "ready-for-humans") {
		t.Errorf("missing plugin name in %q", got)
	}
}
