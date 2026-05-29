package explorer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	logContent := strings.Join([]string{
		`{"msg":"Received event","event_type":"pull_request_review","org":"openshift","repo":"sippy","pr":3557,"time":"2026-05-28T20:47:37Z"}`,
		`{"action":"submitted","duration":"3.2ms","event-GUID":"abc-123","event_type":"pull_request_review","msg":"Plugin completed","org":"openshift","outcome":"acted","plugin":"ready-for-humans","pr":3557,"reason":"added label","repo":"sippy","reviewer":"coderabbitai[bot]","time":"2026-05-28T20:47:37Z"}`,
		`{"action":"submitted","duration":"3µs","event-GUID":"def-456","event_type":"pull_request_review","msg":"Plugin completed","org":"openshift","outcome":"filtered","plugin":"ready-for-humans","pr":3557,"reason":"wrong reviewer","repo":"sippy","reviewer":"neisw","time":"2026-05-28T20:47:21Z"}`,
		`{"action":"submitted","duration":"4µs","event-GUID":"ghi-789","event_type":"pull_request_review","msg":"Plugin completed","org":"openshift","outcome":"no_change","plugin":"ready-for-humans","pr":3557,"reason":"review not approved","repo":"sippy","reviewer":"coderabbitai[bot]","time":"2026-05-28T20:47:30Z"}`,
	}, "\n")

	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")
	if err := os.WriteFile(logFile, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("domain level shows acted and no_change", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowDomain,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "✅") {
			t.Error("should show acted events")
		}
		if !strings.Contains(output, "💤") {
			t.Error("should show no_change events")
		}
		if strings.Contains(output, "🔽") {
			t.Error("should not show filtered events at domain level")
		}
		if !strings.Contains(output, "---") {
			t.Error("should have summary line")
		}
	})

	t.Run("acted level hides no_change", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowActed,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "✅") {
			t.Error("should show acted events")
		}
		if strings.Contains(output, "💤") {
			t.Error("should not show no_change at acted level")
		}
	})

	t.Run("all level shows everything", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowAll,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "🔽") {
			t.Error("should show filtered events at all level")
		}
	})

	t.Run("events are sorted by time", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowAll,
		})
		if err != nil {
			t.Fatal(err)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		// Last line is summary, so check the event lines are time-ordered
		if len(lines) < 3 {
			t.Fatalf("expected at least 3 event lines + summary, got %d", len(lines))
		}
		// First event should be 20:47:21, second 20:47:30, third 20:47:37
		if !strings.Contains(lines[0], "20:47:21") {
			t.Errorf("first line should be earliest event, got %q", lines[0])
		}
		if !strings.Contains(lines[2], "20:47:37") {
			t.Errorf("third line should be latest event, got %q", lines[2])
		}
	})

	t.Run("json output", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowDomain,
			JSON:      true,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, `"outcome":"acted"`) {
			t.Error("JSON output should contain outcome field")
		}
		if strings.Contains(output, "---") {
			t.Error("JSON mode should not print summary")
		}
	})
}

func TestRunVerbose(t *testing.T) {
	logContent := strings.Join([]string{
		`{"action":"submitted","duration":"3.2ms","event-GUID":"abc-123","event_type":"pull_request_review","msg":"Plugin completed","org":"openshift","outcome":"acted","plugin":"ready-for-humans","pr":3557,"reason":"added label","repo":"sippy","reviewer":"coderabbitai[bot]","time":"2026-05-28T20:47:37Z"}`,
		`{"event-GUID":"abc-123","level":"debug","msg":"config resolved","plugin":"ready-for-humans","label":"ready-for-human-review","time":"2026-05-28T20:47:36Z"}`,
		`{"event-GUID":"abc-123","level":"debug","msg":"review state is approved, adding label","plugin":"ready-for-humans","time":"2026-05-28T20:47:36Z"}`,
		`{"event-GUID":"def-456","level":"debug","msg":"unrelated event log","plugin":"ready-for-humans","time":"2026-05-28T20:47:20Z"}`,
	}, "\n")

	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")
	if err := os.WriteFile(logFile, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("verbose shows intermediate logs", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowDomain,
			Verbose:   true,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, "config resolved") {
			t.Error("should show intermediate log line")
		}
		if !strings.Contains(output, "[debug]") {
			t.Error("should show log level")
		}
		if !strings.Contains(output, "review state is approved") {
			t.Error("should show second intermediate log line")
		}
	})

	t.Run("non-verbose hides intermediate logs", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowDomain,
			Verbose:   false,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if strings.Contains(output, "config resolved") {
			t.Error("should not show intermediate log lines without verbose")
		}
	})

	t.Run("orphaned logs without event are not shown", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowDomain,
			Verbose:   true,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if strings.Contains(output, "unrelated event log") {
			t.Error("should not show logs for events without a Plugin completed line")
		}
	})

	t.Run("verbose json includes logs array", func(t *testing.T) {
		var buf bytes.Buffer
		err := Run(&buf, []string{logFile}, Options{
			Plugin:    "ready-for-humans",
			ShowLevel: ShowDomain,
			JSON:      true,
			Verbose:   true,
		})
		if err != nil {
			t.Fatal(err)
		}

		output := buf.String()
		if !strings.Contains(output, `"logs":[`) {
			t.Error("JSON verbose output should contain logs array")
		}
		if !strings.Contains(output, `"message":"config resolved"`) {
			t.Error("JSON verbose output should contain intermediate log message")
		}
	})
}
