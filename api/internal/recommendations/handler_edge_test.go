package recommendations

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerRequestEdges(t *testing.T) {
	router, _ := newTestRouter(t)

	// A chunked request with an empty body has no Content-Length; it still
	// means "no options".
	req := httptest.NewRequest(http.MethodPost, "/api/v1/households/"+hhA+"/autopilot/weeks/2026-W38/generate", io.NopCloser(strings.NewReader("")))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Authorization", "Bearer token-"+userAda)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusCreated, "")
	proposal := decodeBody[ProposalResponse](t, rec)

	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/generate", "  \n", userAda), http.StatusCreated, "")
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/generate", `{"avoidPrevious": false}`, userAda), http.StatusCreated, "")
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/generate", `{"avoid": false}`, userAda), http.StatusBadRequest, "invalid_request")

	// A slot ID that isn't a day is invalid; a day without a meal is not found.
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/slots/monday/swap", `{"version": 3}`, userAda), http.StatusBadRequest, "validation_failed")
	current := decodeBody[ProposalResponse](t, do(t, router, http.MethodGet, "/weeks/2026-W38/proposal", "", userAda))
	if proposal.Week != current.Week {
		t.Fatalf("proposal week = %s", current.Week)
	}
	wantStatus(t, do(t, router, http.MethodPost, "/weeks/2026-W38/proposal/slots/sat/swap", `{"version": 3}`, userAda), http.StatusNotFound, "not_found")

	// A section must be an object: null is not a change.
	wantStatus(t, do(t, router, http.MethodPatch, "/profile", `{"taste": null}`, userAda), http.StatusBadRequest, "validation_failed")
}
