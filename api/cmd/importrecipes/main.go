// Command importrecipes loads a recipe import file (docs/import-format.md)
// directly into MongoDB with the same service the API uses. It is meant for
// the initial bulk load of a household's order history; the
// POST /api/v1/households/{householdId}/recipes/import endpoint does the same
// over HTTP. Re-running it with the same file changes nothing.
//
//	MONGODB_URI=... MONGODB_DATABASE=dinneros \
//	  go run ./cmd/importrecipes -file path/to/recipes.json -household <householdId>
//
// It reads MONGODB_URI (default mongodb://localhost:27017) and MONGODB_DATABASE
// (default dinneros) from the environment and never prints the URI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "importrecipes:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("importrecipes", flag.ContinueOnError)
	flags.SetOutput(out)
	path := flags.String("file", "", "recipe import file (JSON, contract v1)")
	householdID := flags.String("household", "", "ID of the existing household to import into")
	timeout := flags.Duration("timeout", 10*time.Minute, "overall time limit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || *householdID == "" {
		flags.Usage()
		return errors.New("-file and -household are required")
	}
	if _, err := mongodb.ParseID(*householdID); err != nil {
		return errors.New("-household must be a 24-character hex household ID")
	}

	file, err := readImportFile(*path)
	if err != nil {
		return err
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
	client, err := mongodb.Connect(ctx, mongodb.Config{URI: uri, Database: database, AppName: "dinneros-importrecipes"})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close(context.WithoutCancel(ctx)) }()

	householdService := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(client.Database())})
	household, err := householdService.GetHousehold(ctx, *householdID)
	if errors.Is(err, households.ErrNotFound) {
		return fmt.Errorf("household %s does not exist in database %q", *householdID, database)
	}
	if err != nil {
		return fmt.Errorf("look up household: %w", err)
	}
	if err := client.EnsureIndexes(ctx, recipes.Indexes()...); err != nil {
		return err
	}

	fmt.Fprintf(out, "importing %d recipes into household %q (%s), database %q\n", len(file.Recipes), household.Name, household.ID, database)
	res, err := recipes.NewService(recipes.NewMongoStore(client.Database())).Import(ctx, household.ID, file)
	if err != nil {
		return err
	}
	printResult(out, res)
	return nil
}

func readImportFile(path string) (recipes.ImportFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return recipes.ImportFile{}, err
	}
	defer f.Close()
	var file recipes.ImportFile
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return recipes.ImportFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	if dec.More() {
		return recipes.ImportFile{}, fmt.Errorf("read %s: trailing data after the JSON object", path)
	}
	return file, nil
}

func printResult(out io.Writer, res recipes.ImportResult) {
	fmt.Fprintf(out, "recipes: %d created, %d updated, %d unchanged, %d rejected\n", res.Created, res.Updated, res.Unchanged, len(res.Errors))
	fmt.Fprintf(out, "ingredients created: %d\n", res.IngredientsCreated)
	fmt.Fprintf(out, "review items: %d\n", res.ReviewItems)
	for _, e := range res.Errors {
		fmt.Fprintf(out, "  rejected recipes[%d] %s %q: %s\n", e.Index, e.SourceRecipeID, e.Name, strings.Join(e.Problems, "; "))
	}
}
