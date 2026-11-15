// Command sharedcount prints how many recipes Shared Recipes shows before and
// after decision 689, which shows each real recipe once instead of every
// shared copy of it. It only reads: no index, entry, or report is written,
// so it is safe to run against any database, before or after the deploy.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/sharedcount
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/sharedcount -top 20
//
// Groups are computed from each entry's content with today's rules, whether
// or not the server has stored fingerprints yet. "Visible" leaves out
// recipes from a withheld source and recipes reported by enough households
// (decision 686): by entry before, by group after.
//
// It reads MONGODB_URI (default mongodb://localhost:27017) and
// MONGODB_DATABASE (default dinneros) and never prints the URI.
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sharedcount:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("sharedcount", flag.ContinueOnError)
	flags.SetOutput(out)
	top := flags.Int("top", 10, "how many of the largest groups to list")
	timeout := flags.Duration("timeout", 2*time.Minute, "overall time limit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *top < 0 {
		return fmt.Errorf("-top must not be negative")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-sharedcount"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()

	entries, err := catalog.NewMongoStore(client.Database()).ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("read shared recipes: %w", err)
	}
	reporters, err := catalog.NewMongoReports(client.Database()).Reporters(ctx)
	if err != nil {
		return fmt.Errorf("read reports: %w", err)
	}
	fmt.Fprintf(out, "database %q\n", database)
	Print(out, Count(entries, reporters), *top)
	return nil
}

// Group is one recipe and its copies.
type Group struct {
	Key    string
	Name   string
	Copies int
}

// Counts is what Print reports.
type Counts struct {
	// Stored is every entry the lists can show, reported or not.
	Stored int
	// Before is the entries visible before decision 689: one per entry,
	// less those PullAfter households reported.
	Before int
	// Groups is the recipes those entries are; After is the ones visible
	// after decision 689, less groups PullAfter households reported.
	Groups int
	After  int
	// Stale is how many entries the server's startup backfill would update.
	Stale int
	// Largest lists the groups with more than one copy, largest first.
	Largest []Group
}

// Count applies the before and after rules to the stored entries.
func Count(entries []catalog.GroupEntry, reporters map[string][]string) Counts {
	c := Counts{Stored: len(entries)}
	byGroup := map[string]*Group{}
	groupReporters := map[string]map[string]bool{}
	for _, e := range entries {
		if !e.Current {
			c.Stale++
		}
		if len(reporters[e.ID]) < catalog.PullAfter {
			c.Before++
		}
		g := byGroup[e.Group]
		if g == nil {
			g = &Group{Key: e.Group, Name: e.Name}
			byGroup[e.Group] = g
			groupReporters[e.Group] = map[string]bool{}
		}
		g.Copies++
		for _, hh := range reporters[e.ID] {
			groupReporters[e.Group][hh] = true
		}
	}
	c.Groups = len(byGroup)
	for key, g := range byGroup {
		if len(groupReporters[key]) < catalog.PullAfter {
			c.After++
		}
		if g.Copies > 1 {
			c.Largest = append(c.Largest, *g)
		}
	}
	slices.SortFunc(c.Largest, func(a, b Group) int {
		if n := cmp.Compare(b.Copies, a.Copies); n != 0 {
			return n
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return c
}

// Print writes the counts and up to top of the largest groups.
func Print(w io.Writer, c Counts, top int) {
	fmt.Fprintf(w, "stored shared entries: %d\n", c.Stored)
	fmt.Fprintf(w, "shown before dedupe:   %d\n", c.Before)
	fmt.Fprintf(w, "recipes (groups):      %d\n", c.Groups)
	fmt.Fprintf(w, "shown after dedupe:    %d\n", c.After)
	fmt.Fprintf(w, "groups with copies:    %d\n", len(c.Largest))
	fmt.Fprintf(w, "entries the server will fingerprint at startup: %d\n", c.Stale)
	for i, g := range c.Largest {
		if i >= top {
			break
		}
		kind, _, _ := strings.Cut(g.Key, ":")
		fmt.Fprintf(w, "  %3d copies  %-5s  %s\n", g.Copies, kind, g.Name)
	}
}
