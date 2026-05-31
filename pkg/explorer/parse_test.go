package explorer

import (
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		plugin string
		want   *Event
		ok     bool
	}{
		{
			name:   "acted event",
			plugin: "ready-for-humans",
			line:   `{"action":"submitted","component":"ship-hook","duration":"3.2ms","event-GUID":"abc-123","event_type":"pull_request_review","level":"info","msg":"Plugin completed","org":"openshift","outcome":"acted","plugin":"ready-for-humans","pr":3557,"reason":"added ready-for-humans label","repo":"sippy","reviewer":"coderabbitai[bot]","time":"2026-05-28T20:47:37Z","url":"https://github.com/openshift/sippy/pull/3557"}`,
			want: &Event{
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
			},
			ok: true,
		},
		{
			name:   "filtered event with reviewer",
			plugin: "ready-for-humans",
			line:   `{"action":"submitted","duration":"3.252µs","event-GUID":"def-456","event_type":"pull_request_review","level":"debug","msg":"Plugin completed","org":"openshift","outcome":"filtered","plugin":"ready-for-humans","pr":3557,"reason":"reviewer is not the configured bot","repo":"sippy","reviewer":"neisw","time":"2026-05-28T20:47:21Z","url":"https://github.com/openshift/sippy/pull/3557"}`,
			want: &Event{
				Time:      time.Date(2026, 5, 28, 20, 47, 21, 0, time.UTC),
				EventGUID: "def-456",
				EventType: "pull_request_review",
				Org:       "openshift",
				Repo:      "sippy",
				PR:        3557,
				Action:    "submitted",
				Actor:     "neisw",
				URL:       "https://github.com/openshift/sippy/pull/3557",
				Outcome:   OutcomeFiltered,
				Reason:    "reviewer is not the configured bot",
				Duration:  3252 * time.Nanosecond,
			},
			ok: true,
		},
		{
			name:   "wrong plugin is skipped",
			plugin: "ready-for-humans",
			line:   `{"msg":"Plugin completed","plugin":"example","outcome":"skipped","reason":"not implemented"}`,
			ok:     false,
		},
		{
			name:   "non-completion message is skipped",
			plugin: "ready-for-humans",
			line:   `{"msg":"Received event","event_type":"pull_request","org":"openshift","repo":"origin","pr":31217}`,
			ok:     false,
		},
		{
			name:   "invalid JSON is skipped",
			plugin: "ready-for-humans",
			line:   `not json at all`,
			ok:     false,
		},
		{
			name:   "malformed time is skipped",
			plugin: "ready-for-humans",
			line:   `{"msg":"Plugin completed","plugin":"ready-for-humans","outcome":"acted","time":"not-a-time"}`,
			ok:     false,
		},
		{
			name:   "author fallback when no reviewer",
			plugin: "ready-for-humans",
			line:   `{"action":"opened","duration":"1µs","event-GUID":"ghi-789","event_type":"pull_request","level":"trace","msg":"Plugin completed","org":"openshift","outcome":"skipped","plugin":"ready-for-humans","pr":31226,"reason":"only handles review events","repo":"origin","author":"developer","time":"2026-05-28T20:48:00Z"}`,
			want: &Event{
				Time:      time.Date(2026, 5, 28, 20, 48, 0, 0, time.UTC),
				EventGUID: "ghi-789",
				EventType: "pull_request",
				Org:       "openshift",
				Repo:      "origin",
				PR:        31226,
				Action:    "opened",
				Actor:     "developer",
				Outcome:   OutcomeSkipped,
				Reason:    "only handles review events",
				Duration:  time.Microsecond,
			},
			ok: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseLine([]byte(tc.line), tc.plugin)
			if ok != tc.ok {
				t.Fatalf("ParseLine() ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got.EventGUID != tc.want.EventGUID {
				t.Errorf("EventGUID = %q, want %q", got.EventGUID, tc.want.EventGUID)
			}
			if got.Outcome != tc.want.Outcome {
				t.Errorf("Outcome = %q, want %q", got.Outcome, tc.want.Outcome)
			}
			if got.Actor != tc.want.Actor {
				t.Errorf("Actor = %q, want %q", got.Actor, tc.want.Actor)
			}
			if got.Reason != tc.want.Reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.want.Reason)
			}
			if got.Org != tc.want.Org {
				t.Errorf("Org = %q, want %q", got.Org, tc.want.Org)
			}
			if got.Repo != tc.want.Repo {
				t.Errorf("Repo = %q, want %q", got.Repo, tc.want.Repo)
			}
			if got.PR != tc.want.PR {
				t.Errorf("PR = %d, want %d", got.PR, tc.want.PR)
			}
			if !got.Time.Equal(tc.want.Time) {
				t.Errorf("Time = %v, want %v", got.Time, tc.want.Time)
			}
			if got.Duration != tc.want.Duration {
				t.Errorf("Duration = %v, want %v", got.Duration, tc.want.Duration)
			}
		})
	}
}

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		plugin  string
		wantMsg string
		wantLvl string
		wantID  string
		ok      bool
	}{
		{
			name:    "intermediate plugin log",
			plugin:  "ready-for-humans",
			line:    `{"event-GUID":"abc-123","level":"debug","msg":"config resolved","plugin":"ready-for-humans","label":"ready-for-human-review","time":"2026-05-28T20:47:36Z"}`,
			wantMsg: "config resolved",
			wantLvl: "debug",
			wantID:  "abc-123",
			ok:      true,
		},
		{
			name:   "dispatcher message is excluded",
			plugin: "ready-for-humans",
			line:   `{"event-GUID":"abc-123","level":"debug","msg":"Dispatching to plugin","plugin":"ready-for-humans","time":"2026-05-28T20:47:36Z"}`,
			ok:     false,
		},
		{
			name:   "Plugin completed is excluded",
			plugin: "ready-for-humans",
			line:   `{"event-GUID":"abc-123","level":"info","msg":"Plugin completed","outcome":"acted","plugin":"ready-for-humans","time":"2026-05-28T20:47:37Z"}`,
			ok:     false,
		},
		{
			name:   "wrong plugin",
			plugin: "ready-for-humans",
			line:   `{"event-GUID":"abc-123","level":"debug","msg":"some log","plugin":"example","time":"2026-05-28T20:47:36Z"}`,
			ok:     false,
		},
		{
			name:   "no event-GUID",
			plugin: "ready-for-humans",
			line:   `{"level":"debug","msg":"some log","plugin":"ready-for-humans","time":"2026-05-28T20:47:36Z"}`,
			ok:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ll, guid, ok := ParseLogLine([]byte(tc.line), tc.plugin)
			if ok != tc.ok {
				t.Fatalf("ParseLogLine() ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if ll.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", ll.Message, tc.wantMsg)
			}
			if ll.Level != tc.wantLvl {
				t.Errorf("Level = %q, want %q", ll.Level, tc.wantLvl)
			}
			if guid != tc.wantID {
				t.Errorf("GUID = %q, want %q", guid, tc.wantID)
			}
		})
	}
}

func TestParseLogLineExtraFields(t *testing.T) {
	line := `{"event-GUID":"abc-123","level":"debug","msg":"config resolved","plugin":"ready-for-humans","label":"ready-for-human-review","bot":"coderabbitai[bot]","time":"2026-05-28T20:47:36Z"}`
	ll, _, ok := ParseLogLine([]byte(line), "ready-for-humans")
	if !ok {
		t.Fatal("expected ok")
	}
	if ll.Fields["label"] != "ready-for-human-review" {
		t.Errorf("expected label field, got %v", ll.Fields)
	}
	if ll.Fields["bot"] != "coderabbitai[bot]" {
		t.Errorf("expected bot field, got %v", ll.Fields)
	}
}
