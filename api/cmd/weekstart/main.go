// Command weekstart moves households that never chose a first day of the week
// (their weeks start on Monday, as ISO weeks did) to Sunday-first weeks, the
// default for new households. It changes the setting through the same path as
// Household Settings: scheduled meals whose date now falls in the neighboring
// week move into that week, so no meal changes date (docs/architecture.md,
// decision 503). Households that chose a first day are never touched.
//
// It is a dry run unless -apply is given, and re-running it is safe.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/weekstart -all          # dry run
//	MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/weekstart -household <id> -apply
//
// -to picks another first day (sun, mon, …, sat). It reads MONGODB_URI
// (default mongodb://localhost:27017) and MONGODB_DATABASE (default dinneros)
// from the environment and never prints the URI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "weekstart:", err)
		os.Exit(1)
	}
}

const pageSize = 200

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("weekstart", flag.ContinueOnError)
	flags.SetOutput(out)
	household := flags.String("household", "", "ID of one household to move")
	all := flags.Bool("all", false, "move every household that never chose a first day")
	to := flags.String("to", households.DefaultWeekStart, "first day of the week to move to (sun, mon, tue, wed, thu, fri, sat)")
	apply := flags.Bool("apply", false, "write changes (default is a dry run)")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall time limit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	*household = strings.ToLower(strings.TrimSpace(*household))
	if (*household == "") == !*all {
		flags.Usage()
		return errors.New("give exactly one of -household or -all")
	}
	if *household != "" {
		if _, err := mongodb.ParseID(*household); err != nil {
			return errors.New("-household must be a 24-character hex household ID")
		}
	}
	if !slices.Contains(households.OrderDays, *to) {
		return errors.New("-to must be one of sun, mon, tue, wed, thu, fri, sat")
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
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-weekstart"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()

	householdStore := households.NewMongoStore(client.Database())
	planStore := planning.NewMongoStore(client.Database())
	plans := planning.NewService(planStore, nil)
	service := households.NewService(households.ServiceOptions{Store: householdStore, OnWeekStart: plans})

	ids := []string{*household}
	if *all {
		ids = nil
		for after := ""; ; {
			page, err := householdStore.ListHouseholdIDs(ctx, after, pageSize)
			if err != nil {
				return err
			}
			ids = append(ids, page...)
			if len(page) < pageSize {
				break
			}
			after = page[len(page)-1]
		}
	}

	changed, skipped, meals := 0, 0, 0
	for _, id := range ids {
		hh, err := householdStore.GetHousehold(ctx, id)
		if errors.Is(err, households.ErrNotFound) {
			return fmt.Errorf("household %s not found in database %q", id, database)
		}
		if err != nil {
			return fmt.Errorf("look up household %s: %w", id, err)
		}
		if hh.WeekStartsOn != "" {
			skipped++
			fmt.Fprintf(out, "household %s: already chose %s, left alone\n", id, hh.WeekStartsOn)
			continue
		}
		n, err := planStore.CountEntriesForWeekStart(ctx, id, planning.Day(hh.FirstDay()), planning.Day(*to))
		if err != nil {
			return fmt.Errorf("household %s: %w", id, err)
		}
		verb := "would move"
		if *apply {
			// An admin's change, as if made in Household Settings.
			actor := households.Membership{HouseholdID: id, Role: households.RoleAdmin}
			if _, err := service.Update(ctx, actor, households.UpdateInput{WeekStartsOn: to}); err != nil {
				return fmt.Errorf("household %s: %w", id, err)
			}
			verb = "moved"
		}
		changed++
		meals += n
		fmt.Fprintf(out, "household %s: %s → %s, %s %d scheduled meals to the neighboring week\n", id, hh.FirstDay(), *to, verb, n)
	}
	fmt.Fprintf(out, "database %q: %d households to %s, %d meals, %d left alone\n", database, changed, *to, meals, skipped)
	if !*apply && changed > 0 {
		fmt.Fprintln(out, "dry run: re-run with -apply to write")
	}
	return nil
}
