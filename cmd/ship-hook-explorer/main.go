package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/openshift-eng/ship-hook/pkg/explorer"
)

func main() {
	plugin := flag.String("plugin", "", "Plugin name to focus on (required)")
	show := flag.String("show", "domain", "Show level: acted, domain, all")
	org := flag.String("org", "", "Filter by GitHub org")
	repo := flag.String("repo", "", "Filter by repo")
	pr := flag.Int("pr", 0, "Filter by PR number")
	since := flag.String("since", "", "Events after this time (RFC 3339 or YYYY-MM-DD)")
	until := flag.String("until", "", "Events before this time (RFC 3339 or YYYY-MM-DD)")
	jsonOutput := flag.Bool("json", false, "JSON output per matched event")
	verbose := flag.Bool("verbose", false, "Show intermediate plugin log lines under each event")
	flag.BoolVar(verbose, "v", false, "Show intermediate plugin log lines under each event")

	flag.Parse()

	if *plugin == "" {
		fmt.Fprintln(os.Stderr, "error: --plugin is required")
		flag.Usage()
		os.Exit(1)
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one log file is required (use - for stdin)")
		flag.Usage()
		os.Exit(1)
	}

	showLevel, ok := explorer.ParseShowLevel(*show)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: invalid --show value %q (use acted, domain, or all)\n", *show)
		os.Exit(1)
	}

	filter := explorer.Filter{
		Org:  *org,
		Repo: *repo,
		PR:   *pr,
	}

	if *since != "" {
		t, err := parseTime(*since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --since value %q: %v\n", *since, err)
			os.Exit(1)
		}
		filter.Since = t
	}

	if *until != "" {
		t, err := parseTime(*until)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --until value %q: %v\n", *until, err)
			os.Exit(1)
		}
		filter.Until = t
	}

	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		fmt.Fprintln(os.Stderr, "error: --since must not be after --until")
		os.Exit(1)
	}

	opts := explorer.Options{
		Plugin:    *plugin,
		ShowLevel: showLevel,
		Filter:    filter,
		JSON:      *jsonOutput,
		Verbose:   *verbose,
	}

	if err := explorer.Run(os.Stdout, files, opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC 3339 or YYYY-MM-DD format")
}
