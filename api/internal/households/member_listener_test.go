package households

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type memberRecorder struct {
	heard  []MemberAdded
	proofs []DeviceProof
}

func (m *memberRecorder) MemberAdded(ctx context.Context, a MemberAdded) {
	m.heard = append(m.heard, a)
	p, _ := DeviceProofFromContext(ctx)
	m.proofs = append(m.proofs, p)
}

// Creating a household and adding a member both tell the listener, with the
// request's DeviceCheck token; re-adding an existing member doesn't.
func TestMemberListenerHearsNewMemberships(t *testing.T) {
	heard := &memberRecorder{}
	svc := NewService(ServiceOptions{Store: newMemoryStore(), OnMember: heard, Now: func() time.Time { return testNow }})
	ctx := WithDeviceProof(context.Background(), DeviceProof{Token: "dGVzdA==", Development: true})
	h, _, err := svc.Create(ctx, userAda, CreateInput{Name: "Home", TimeZone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AddMember(context.Background(), h.ID, userBob, RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AddMember(context.Background(), h.ID, userBob, RoleMember); err != nil {
		t.Fatal(err)
	}
	if len(heard.heard) != 2 {
		t.Fatalf("heard %+v, want create and one join", heard.heard)
	}
	if c := heard.heard[0]; !c.Created || c.UserID != userAda || c.HouseholdID != h.ID || !c.At.Equal(testNow) {
		t.Errorf("create = %+v", c)
	}
	if j := heard.heard[1]; j.Created || j.UserID != userBob {
		t.Errorf("join = %+v", j)
	}
	if p := heard.proofs[0]; p.Token != "dGVzdA==" || !p.Development {
		t.Errorf("create proof = %+v", p)
	}
	if p := heard.proofs[1]; p.Token != "" {
		t.Errorf("join proof = %+v", p)
	}
}

// POST /households takes an optional deviceCheck object and passes it on.
func TestCreateHouseholdRouteTakesADeviceCheckToken(t *testing.T) {
	heard := &memberRecorder{}
	svc := NewService(ServiceOptions{Store: newMemoryStore(), OnMember: heard, Now: func() time.Time { return testNow }})
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Tokens: fakeTokens{}}).Mount)
	for _, body := range []string{
		`{"name":"Home","timeZone":"UTC","deviceCheck":{"token":"abc","development":true}}`,
		`{"name":"Cabin","timeZone":"UTC"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/households", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userAda)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
		}
	}
	if len(heard.proofs) != 2 || heard.proofs[0].Token != "abc" || !heard.proofs[0].Development || heard.proofs[1].Token != "" {
		t.Errorf("proofs = %+v", heard.proofs)
	}
	long := DeviceCheckRequest{Token: strings.Repeat("a", MaxDeviceTokenLength+1)}
	if _, ok := long.Proof(); ok {
		t.Error("an overlong token was accepted")
	}
}
