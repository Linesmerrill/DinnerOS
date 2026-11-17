// Command offercodes stocks the invite free month code pool from an App
// Store Connect download, straight into MongoDB (decision 755). It reads the
// CSV on standard input and prints counts only, never a code:
//
//	heroku run --no-tty -a dinneros-api -- /offercodes -offer "Invite free month" \
//	    -batch 2026-11-a -expires 2027-02-15 < codes.csv
//	heroku run -a dinneros-api -- /offercodes -stock
//
// Months waiting for a code get theirs at the API's next sweep, within the
// hour, with their notification. The operator route
// (POST /api/v1/admin/plus/offer-codes, scripts/offer-codes.sh) gives them
// at once.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, in io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("offercodes", flag.ContinueOnError)
	flags.SetOutput(out)
	offer := flags.String("offer", "", "the offer's reference name in App Store Connect")
	batch := flags.String("batch", "", "a name for this download, like 2026-11-a")
	expires := flags.String("expires", "", "the day the codes expire, like 2027-02-15")
	stock := flags.Bool("stock", false, "print the pool's counts and import nothing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	b := plus.OfferBatch{Offer: strings.TrimSpace(*offer), Batch: strings.TrimSpace(*batch)}
	var codes []string
	skipped := 0
	if !*stock {
		if b.Offer == "" || b.Batch == "" {
			return errors.New("give -offer and -batch")
		}
		day, err := time.Parse(time.DateOnly, strings.TrimSpace(*expires))
		if err != nil {
			return errors.New("give -expires as a date, like 2027-02-15")
		}
		// The codes work through that day.
		b.ExpiresAt = day.Add(24*time.Hour - time.Second).UTC()
		if !b.ExpiresAt.After(time.Now()) {
			return errors.New("those codes have already expired")
		}
		raw, err := io.ReadAll(io.LimitReader(in, 64<<20))
		if err != nil {
			return fmt.Errorf("read the codes: %w", err)
		}
		if codes, skipped = plus.ParseOfferCodes(string(raw)); len(codes) == 0 {
			return errors.New("no codes on standard input")
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	uri := strings.TrimSpace(getenv("MONGODB_URI"))
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	database := strings.TrimSpace(getenv("MONGODB_DATABASE"))
	if database == "" {
		database = "dinneros"
	}
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-offercodes"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()
	if err := client.EnsureIndexes(ctx, plus.OfferCodeIndexes()...); err != nil {
		return err
	}
	store := plus.NewMongoOfferCodes(client.Database())
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if !*stock {
		res, err := store.Import(ctx, codes, b, time.Now().UTC())
		if err != nil {
			return err
		}
		res.Skipped = skipped
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
	st, err := store.Stock(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	return enc.Encode(st)
}
