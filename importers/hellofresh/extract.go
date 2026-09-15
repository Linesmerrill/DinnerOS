package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var (
	// ErrNoPageData means the page did not contain the embedded Next.js data.
	ErrNoPageData = errors.New("page has no embedded __NEXT_DATA__")
	// ErrNoRecipe means the page data did not contain a recipe.
	ErrNoRecipe = errors.New("page data contains no recipe")

	nextDataRe = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json"[^>]*>([\s\S]*?)</script>`)
)

// droppedRecipeFields are large or user-generated fields that DinnerOS does not
// use. Reviews in particular are other people's content and are not stored.
var droppedRecipeFields = []string{"reviews", "descriptionHTML", "videoMetadata"}

// ExtractRecipe returns the recipe object embedded in a HelloFresh recipe page.
func ExtractRecipe(html []byte) (json.RawMessage, error) {
	m := nextDataRe.FindSubmatch(html)
	if m == nil {
		return nil, ErrNoPageData
	}

	var page struct {
		Props struct {
			PageProps struct {
				SSRPayload struct {
					Recipe json.RawMessage `json:"recipe"`
				} `json:"ssrPayload"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(m[1], &page); err != nil {
		return nil, fmt.Errorf("parse page data: %w", err)
	}
	raw := bytes.TrimSpace(page.Props.PageProps.SSRPayload.Recipe)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, ErrNoRecipe
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("parse recipe: %w", err)
	}
	for _, k := range droppedRecipeFields {
		delete(fields, k)
	}
	return json.Marshal(fields)
}
