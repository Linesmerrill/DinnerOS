// Command metrics prints DinnerOS's weekly numbers: active households, carts
// sent, the average cart value per store (decision 674), and who has Plus
// (decision 675). It only reads.
//
//	heroku run /metrics -weeks 8 -a dinneros-api
package main

import (
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

	"github.com/Linesmerrill/DinnerOS/api/internal/metrics"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "metrics:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("metrics", flag.ContinueOnError)
	flags.SetOutput(out)
	weeks := flags.Int("weeks", 8, "how many weeks to show, ending with this one")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *weeks < 1 || *weeks > 104 {
		return fmt.Errorf("-weeks must be between 1 and 104")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-metrics"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()

	report, err := metrics.Weeks(ctx, client.Database(), time.Now(), *weeks)
	if err != nil {
		return err
	}
	Print(out, report)
	now := time.Now()
	p, err := metrics.PlusNow(ctx, client.Database(), now, now.AddDate(0, 0, -7*(*weeks)))
	if err != nil {
		return err
	}
	PrintPlus(out, report, p)
	return nil
}

// PrintPlus writes who has Plus now and the codes used each week.
func PrintPlus(out io.Writer, report []metrics.Week, p metrics.Plus) {
	fmt.Fprintf(out, "\nPlus now: %d paying, %d in trial, %d on free months\n", p.Paying, p.Trial, p.FreeMonths)
	var parts []string
	for _, w := range report {
		parts = append(parts, fmt.Sprintf("%s %d", w.Week, p.Referrals[w.Week]))
	}
	fmt.Fprintf(out, "Referral codes used: %s\n", strings.Join(parts, ", "))
}

// Print writes the report as a plain table.
func Print(out io.Writer, report []metrics.Week) {
	fmt.Fprintf(out, "%-9s %8s %6s  %s\n", "week", "active", "carts", "per store: carts, average cart (priced carts)")
	for _, w := range report {
		var stores []string
		for name := range w.Stores {
			stores = append(stores, name)
		}
		slices.Sort(stores)
		var parts []string
		for _, name := range stores {
			s := w.Stores[name]
			avg := "no prices"
			if s.PricedCarts > 0 {
				avg = fmt.Sprintf("$%d.%02d (%d)", s.AverageCents()/100, s.AverageCents()%100, s.PricedCarts)
			}
			parts = append(parts, fmt.Sprintf("%s %d, %s", name, s.Carts, avg))
		}
		fmt.Fprintf(out, "%-9s %8d %6d  %s\n", w.Week, w.ActiveHouseholds, w.CartsSent, strings.Join(parts, "; "))
	}
}
