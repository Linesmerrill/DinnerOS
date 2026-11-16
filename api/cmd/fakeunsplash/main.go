// Command fakeunsplash stands in for the Unsplash API in local QA (decision
// 705), so photo search can be tested without an access key. Point a local
// API at it with UNSPLASH_API_BASE=http://localhost:8099 and any
// UNSPLASH_ACCESS_KEY. It answers a search with a few real, public Unsplash
// image addresses under a made-up photographer, serves a photo by ID, and
// counts download_location calls at GET /qa/downloads.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

var photos = []string{
	"1504674900247-0877df9cc836", "1512621776951-a57141f2eefd", "1546069901-ba9599a7e63c",
	"1565299624946-b28f40a0ae38", "1540189549336-e6e99c3679fe", "1567620905732-2d1ec7ab7445",
}

func main() {
	addr := flag.String("addr", "localhost:8099", "where to listen")
	flag.Parse()
	base := "http://" + *addr
	var mu sync.Mutex
	downloads := map[string]int{}

	photo := func(i int) map[string]any {
		id := fmt.Sprintf("qa%d", i)
		img := "https://images.unsplash.com/photo-" + photos[i]
		return map[string]any{
			"id": id, "width": 4000, "height": 3000,
			"urls":  map[string]string{"regular": img + "?ixid=qa&w=1080", "small": img + "?ixid=qa&w=400"},
			"links": map[string]string{"download_location": base + "/photos/" + id + "/download?ixid=qa"},
			"user": map[string]any{
				"name": "QA Photographer", "username": "qa_photographer",
				"links": map[string]string{"html": "https://unsplash.com/@qa_photographer"},
			},
		}
	}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/photos", func(w http.ResponseWriter, r *http.Request) {
		results := make([]map[string]any, 0, len(photos))
		for i := range photos {
			results = append(results, photo(i))
		}
		write(w, map[string]any{"total": len(photos), "total_pages": 1, "results": results})
	})
	mux.HandleFunc("GET /photos/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		for i := range photos {
			if fmt.Sprintf("qa%d", i) == id {
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
