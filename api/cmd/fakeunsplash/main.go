// Command fakeunsplash stands in for the Unsplash API in local QA (decision
// 705), so photo search can be tested without an access key. Point a local
// API at it with UNSPLASH_API_BASE=http://localhost:8099 and any
// UNSPLASH_ACCESS_KEY. It answers a search with a few real, public Unsplash
// image addresses under a made-up photographer, serves a photo by ID, and
// counts download_location calls at GET /qa/downloads.
//
// With -photos it answers instead with the photos in that file (a list of
// {"recipe", "id", "image", "photographer", "username"}, like
// docs/review-account/photos.json): real Unsplash photos under their real
// photographers, the ones whose recipe shares a word with the search first.
// The App Store screenshots are taken that way.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

// listed is one photo from a -photos file.
type listed struct {
	Recipe       string `json:"recipe"`
	ID           string `json:"id"`
	Image        string `json:"image"`
	Photographer string `json:"photographer"`
	Username     string `json:"username"`
}

var photos = []string{
	"1504674900247-0877df9cc836", "1512621776951-a57141f2eefd", "1546069901-ba9599a7e63c",
	"1565299624946-b28f40a0ae38", "1540189549336-e6e99c3679fe", "1567620905732-2d1ec7ab7445",
}

func main() {
	addr := flag.String("addr", "localhost:8099", "where to listen")
	file := flag.String("photos", "", "a JSON list of real photos to answer with")
	flag.Parse()
	var list []listed
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.Unmarshal(data, &list); err != nil || len(list) == 0 {
			log.Fatalf("read %s: %v", *file, err)
		}
	}
	base := "http://" + *addr
	var mu sync.Mutex
	downloads := map[string]int{}

	count := len(photos)
	if len(list) > 0 {
		count = len(list)
	}
	photo := func(i int) map[string]any {
		id := fmt.Sprintf("qa%d", i)
		img := "https://images.unsplash.com/photo-" + photos[min(i, len(photos)-1)]
		name, username := "QA Photographer", "qa_photographer"
		if len(list) > 0 {
			id, img, name, username = list[i].ID, list[i].Image, list[i].Photographer, list[i].Username
		}
		return map[string]any{
			"id": id, "width": 4000, "height": 3000,
			"urls":  map[string]string{"regular": img + "?ixid=qa&w=1080", "small": img + "?ixid=qa&w=400"},
			"links": map[string]string{"download_location": base + "/photos/" + id + "/download?ixid=qa"},
			"user": map[string]any{
				"name": name, "username": username,
				"links": map[string]string{"html": "https://unsplash.com/@" + username},
			},
		}
	}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/photos", func(w http.ResponseWriter, r *http.Request) {
		results := make([]map[string]any, 0, count)
		for _, i := range order(list, r.URL.Query().Get("query"), count) {
			results = append(results, photo(i))
		}
		write(w, map[string]any{"total": count, "total_pages": 1, "results": results})
	})
	mux.HandleFunc("GET /photos/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		for i := range count {
			if photo(i)["id"] == id {
				write(w, photo(i))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /photos/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		downloads[r.PathValue("id")]++
		mu.Unlock()
		write(w, map[string]string{"url": "ok"})
	})
	mux.HandleFunc("GET /qa/downloads", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		write(w, downloads)
	})
	log.Printf("fake Unsplash on %s", base)
	log.Fatal(http.ListenAndServe(*addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Client-ID ") && !strings.HasPrefix(r.URL.Path, "/qa/") {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})))
}

// order lists the photos with those whose recipe shares a word with the
// query first, so a recipe's own photo leads its search.
func order(list []listed, query string, count int) []int {
	words := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if len(w) > 3 && w != "with" {
			words[w] = true
		}
	}
	var first, rest []int
	for i := range count {
		match := false
		if i < len(list) {
			for _, w := range strings.Fields(strings.ToLower(list[i].Recipe)) {
				match = match || words[w]
			}
		}
		if match {
			first = append(first, i)
		} else {
			rest = append(rest, i)
		}
	}
	return append(first, rest...)
}
