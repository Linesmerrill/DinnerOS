package copytext

import "testing"

func TestCurly(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"No apostrophes here.", "No apostrophes here."},
		{"You've reached the free-month limit.", "You’ve reached the free-month limit."},
		{"Chef's Picks This Week", "Chef’s Picks This Week"},
		{"That store isn't answering. Don't wait.", "That store isn’t answering. Don’t wait."},
		{"Trader Joe's", "Trader Joe’s"},
		{"Already curly: don’t", "Already curly: don’t"},
		{"Say 'hello' to it", "Say 'hello' to it"},
		{"a lone ' mark", "a lone ' mark"},
		{"rock 'n' roll", "rock 'n' roll"},
		{"the 90's", "the 90’s"},
		{"end'", "end'"},
	} {
		if got := Curly(tc.in); got != tc.want {
			t.Errorf("Curly(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
