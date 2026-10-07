package difflib

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// Expected values come from running Python's difflib on the same inputs.

var nfl = strings.Fields("ARI ATL BAL BUF CAR CHI CIN CLE DAL DEN DET GB HOU IND JAX KC LV LAC LAR MIA MIN NE NO NYG NYJ PHI PIT SF SEA TB TEN WSH")

func TestRatio(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"abcab", "cabxab", 0.5454545454545454},
		{"", "", 1},
		{"ab", "", 0},
		{"BUF", "BUFF", 6.0 / 7},
		{"héllo", "hello", 0.8}, // by rune, not byte
	} {
		if got := Ratio(c.a, c.b); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("Ratio(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestGetCloseMatches(t *testing.T) {
	for _, c := range []struct {
		word string
		want []string
	}{
		{"BUFF", []string{"BUF"}},
		{"BU", []string{"BUF"}},
		{"TBB", []string{"TB"}},
		{"AT", []string{"ATL"}},
		// Equal scores: the larger string comes first.
		{"NY", []string{"NYJ", "NYG"}},
		{"LA", []string{"LAR", "LAC"}},
		// Nothing reaches the 0.6 cutoff.
		{"ZZZZ", nil},
	} {
		if got := GetCloseMatches(c.word, nfl, 3, 0.6); !reflect.DeepEqual(got, c.want) {
			t.Errorf("GetCloseMatches(%q) = %v, want %v", c.word, got, c.want)
		}
	}
}

func TestGetCloseMatchesLimitAndOrder(t *testing.T) {
	// Four equal scores, n = 3: the three largest strings, largest first.
	got := GetCloseMatches("ab", []string{"ab1", "ab2", "ab3", "ab4", "xab"}, 3, 0.6)
	if want := []string{"xab", "ab4", "ab3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// A higher score beats a larger string.
	got = GetCloseMatches("appel", []string{"ape", "apple", "peach", "puppy"}, 3, 0.6)
	if want := []string{"apple", "ape"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := GetCloseMatches("ab", nfl, 0, 0.6); got != nil {
		t.Errorf("n=0 returned %v", got)
	}
}

func TestCutoffIsInclusive(t *testing.T) {
	if got := GetCloseMatches("abc", []string{"abd", "axx"}, 3, 2.0/3); !reflect.DeepEqual(got, []string{"abd"}) {
		t.Errorf("got %v", got)
	}
}
