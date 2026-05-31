package explorer

import (
	"encoding/json"
	"time"
)

func ParseLine(line []byte, plugin string) (*Event, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, false
	}

	msg, _ := raw["msg"].(string)
	if msg != "Plugin completed" {
		return nil, false
	}

	p, _ := raw["plugin"].(string)
	if p != plugin {
		return nil, false
	}

	ev := &Event{}

	if t, ok := raw["time"].(string); ok {
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return nil, false
		}
		ev.Time = parsed
	}
	ev.EventGUID, _ = raw["event-GUID"].(string)
	ev.EventType, _ = raw["event_type"].(string)
	ev.Org, _ = raw["org"].(string)
	ev.Repo, _ = raw["repo"].(string)
	if pr, ok := raw["pr"].(float64); ok {
		ev.PR = int(pr)
	}
	ev.Action, _ = raw["action"].(string)
	ev.URL, _ = raw["url"].(string)

	if reviewer, ok := raw["reviewer"].(string); ok && reviewer != "" {
		ev.Actor = reviewer
	} else if author, ok := raw["author"].(string); ok {
		ev.Actor = author
	}

	if outcome, ok := raw["outcome"].(string); ok {
		ev.Outcome = Outcome(outcome)
	}
	ev.Reason, _ = raw["reason"].(string)

	if d, ok := raw["duration"].(string); ok {
		ev.Duration, _ = time.ParseDuration(d)
	}

	return ev, true
}

var dispatcherMessages = map[string]bool{
	"Plugin completed":       true,
	"Dispatching to plugin":  true,
	"Received event":         true,
	"Registering sub-plugin": true,
}

func ParseLogLine(line []byte, plugin string) (*LogLine, string, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, "", false
	}

	p, _ := raw["plugin"].(string)
	if p != plugin {
		return nil, "", false
	}

	guid, _ := raw["event-GUID"].(string)
	if guid == "" {
		return nil, "", false
	}

	msg, _ := raw["msg"].(string)
	if dispatcherMessages[msg] {
		return nil, "", false
	}

	ll := &LogLine{
		Message: msg,
		Level:   stringField(raw, "level"),
	}
	if t, ok := raw["time"].(string); ok {
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return nil, "", false
		}
		ll.Time = parsed
	}

	contextFields := map[string]bool{
		"msg": true, "level": true, "time": true, "plugin": true,
		"event-GUID": true, "event-type": true, "event_type": true,
		"org": true, "repo": true, "pr": true, "action": true,
		"author": true, "reviewer": true, "url": true,
		"component": true, "file": true, "func": true,
		"severity": true, "review": true,
	}

	fields := make(map[string]any)
	for k, v := range raw {
		if !contextFields[k] {
			fields[k] = v
		}
	}
	if len(fields) > 0 {
		ll.Fields = fields
	}

	return ll, guid, true
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
