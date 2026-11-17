package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationOfferCodesImportPrintsCountsOnly(t *testing.T) {
	client := mongotest.Client(t)
	env := func(k string) string {
		switch k {
		case "MONGODB_URI":
			return os.Getenv("MONGODB_TEST_URI")
		case "MONGODB_DATABASE":
			return client.Database().Name()
		}
		return ""
	}
	csv := "Code\nCMDX12345678\nCMDY12345678\nCMDX12345678\n"
	var out bytes.Buffer
	args := []string{"-offer", "Invite free month", "-batch", "test", "-expires", "2099-01-01"}
	if err := run(t.Context(), args, env, strings.NewReader(csv), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"added": 2`) || !strings.Contains(out.String(), `"available": 2`) {
		t.Errorf("output = %s", out.String())
	}
	if strings.Contains(out.String(), "CMDX") {
		t.Error("the output holds a code")
	}
	out.Reset()
	if err := run(t.Context(), args, env, strings.NewReader(csv), &out); err != nil || !strings.Contains(out.String(), `"duplicates": 2`) {
		t.Errorf("again = %s, %v", out.String(), err)
	}
	for name, a := range map[string][]string{
		"no offer": {"-batch", "b", "-expires", "2099-01-01"},
		"expired":  {"-offer", "o", "-batch", "b", "-expires", "2020-01-01"},
		"bad date": {"-offer", "o", "-batch", "b", "-expires", "soon"},
	} {
		if err := run(t.Context(), a, env, strings.NewReader(csv), &out); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := run(t.Context(), []string{"-offer", "o", "-batch", "b", "-expires", "2099-01-01"}, env, strings.NewReader("\n"), &out); err == nil {
		t.Error("an empty file imported")
	}
}
