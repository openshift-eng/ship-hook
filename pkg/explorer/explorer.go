package explorer

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

type Options struct {
	Plugin    string
	ShowLevel ShowLevel
	Filter    Filter
	JSON      bool
	Verbose   bool
}

func Run(w io.Writer, files []string, opts Options) error {
	var events []*Event
	var logs []parsedLog

	for _, file := range files {
		fileEvents, fileLogs, err := readFile(file, opts)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		events = append(events, fileEvents...)
		logs = append(logs, fileLogs...)
	}

	if opts.Verbose {
		groupLogs(events, logs)
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Time.Before(events[j].Time)
	})

	summary := &Summary{Plugin: opts.Plugin}

	var tw *tabwriter.Writer
	if !opts.JSON {
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	}

	for _, ev := range events {
		summary.Add(ev.Outcome)
		if !opts.Filter.Matches(ev) {
			continue
		}
		if !ShouldShow(ev.Outcome, opts.ShowLevel) {
			continue
		}
		summary.Shown++
		if opts.JSON {
			if err := FormatEventJSON(w, ev, opts.Verbose); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(tw, FormatEvent(ev)); err != nil {
				return err
			}
			if opts.Verbose {
				for _, ll := range ev.Logs {
					if _, err := fmt.Fprintln(tw, FormatLogLine(&ll)); err != nil {
						return err
					}
				}
			}
		}
	}

	if !opts.JSON {
		if err := tw.Flush(); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, summary.String()); err != nil {
			return err
		}
	}

	return nil
}

type parsedLog struct {
	guid string
	line LogLine
}

func groupLogs(events []*Event, logs []parsedLog) {
	byGUID := make(map[string][]*Event, len(events))
	for _, ev := range events {
		byGUID[ev.EventGUID] = append(byGUID[ev.EventGUID], ev)
	}
	for _, pl := range logs {
		for _, ev := range byGUID[pl.guid] {
			ev.Logs = append(ev.Logs, pl.line)
		}
	}
	for _, ev := range events {
		sort.Slice(ev.Logs, func(i, j int) bool {
			return ev.Logs[i].Time.Before(ev.Logs[j].Time)
		})
	}
}

func readFile(path string, opts Options) ([]*Event, []parsedLog, error) {
	var reader io.Reader

	if path == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = f.Close() }()

		if strings.HasSuffix(path, ".gz") {
			gz, err := gzip.NewReader(f)
			if err != nil {
				return nil, nil, fmt.Errorf("gzip: %w", err)
			}
			defer func() { _ = gz.Close() }()
			reader = gz
		} else {
			reader = f
		}
	}

	return scanLines(reader, opts)
}

func scanLines(r io.Reader, opts Options) ([]*Event, []parsedLog, error) {
	var events []*Event
	var logs []parsedLog
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if ev, ok := ParseLine(line, opts.Plugin); ok {
			events = append(events, ev)
			continue
		}
		if opts.Verbose {
			if ll, guid, ok := ParseLogLine(line, opts.Plugin); ok {
				logs = append(logs, parsedLog{guid: guid, line: *ll})
			}
		}
	}
	return events, logs, scanner.Err()
}
