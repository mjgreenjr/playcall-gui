// Package difflib ports the parts of Python's difflib that playcall uses:
// SequenceMatcher.ratio and get_close_matches. Strings are compared by rune,
// like Python compares code points.
//
// Not ported: SequenceMatcher's "autojunk" heuristic, which only applies when
// the second sequence has 200 or more elements. Team abbreviations never do.
package difflib

import "sort"

// Ratio is SequenceMatcher(None, a, b).ratio(): 2*M/T, where M is the number
// of matching characters found by Ratcliff/Obershelp (repeatedly take the
// longest matching block, then recurse left and right of it) and T is the
// total length. Two empty strings give 1.0.
func Ratio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	total := len(ra) + len(rb)
	if total == 0 {
		return 1.0
	}
	m := matches(ra, rb, 0, len(ra), 0, len(rb))
	return 2.0 * float64(m) / float64(total)
}

// matches counts matching characters in a[alo:ahi] and b[blo:bhi].
func matches(a, b []rune, alo, ahi, blo, bhi int) int {
	i, j, k := longestMatch(a, b, alo, ahi, blo, bhi)
	if k == 0 {
		return 0
	}
	return k + matches(a, b, alo, i, blo, j) + matches(a, b, i+k, ahi, j+k, bhi)
}

// longestMatch is find_longest_match without junk handling: the longest
// block a[i:i+k] == b[j:j+k] inside the ranges; among equals the one that
// starts earliest in a, then earliest in b.
func longestMatch(a, b []rune, alo, ahi, blo, bhi int) (besti, bestj, bestsize int) {
	besti, bestj = alo, blo
	// j2len[j+1] is the length of the match ending at a[i-1], b[j].
	j2len := make([]int, len(b)+1)
	newj2len := make([]int, len(b)+1)
	for i := alo; i < ahi; i++ {
		for j := range newj2len {
			newj2len[j] = 0
		}
		for j := blo; j < bhi; j++ {
			if a[i] != b[j] {
				continue
			}
			k := j2len[j] + 1
			newj2len[j+1] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len, newj2len = newj2len, j2len
	}
	return besti, bestj, bestsize
}

// GetCloseMatches is difflib.get_close_matches(word, possibilities, n, cutoff):
// the (at most n) possibilities whose ratio to word is at least cutoff, best
// first. Python picks them with heapq.nlargest on (score, string) tuples, so
// equal scores are ordered by string, the larger string first. Python's default
// cutoff is 0.6. n <= 0 returns nothing (Python raises ValueError for n <= 0).
func GetCloseMatches(word string, possibilities []string, n int, cutoff float64) []string {
	if n <= 0 {
		return nil
	}
	type scored struct {
		score float64
		s     string
	}
	var found []scored
	for _, x := range possibilities {
		// Python's SequenceMatcher has word as seq2 and the possibility as seq1.
		if r := Ratio(x, word); r >= cutoff {
			found = append(found, scored{r, x})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].s > found[j].s
	})
	if len(found) > n {
		found = found[:n]
	}
	if len(found) == 0 {
		return nil
	}
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.s
	}
	return out
}
