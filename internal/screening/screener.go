// Package screening checks a person's name against a sanctions list.
//
// Real screening providers do much more (aliases, dates of birth,
// transliteration tables, phonetic matching). This package keeps the core
// idea small: normalize both names the same way, then measure how similar
// they are.
package screening

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Result is the outcome of screening one name.
type Result struct {
	Match       bool
	MatchedName string
	Score       float64 // 0..1, 1 means identical after normalization
	ListVersion string  // which version of the list was used (audit trail)
}

type entry struct {
	original   string
	normalized string
}

// Screener compares names against an in-memory list.
type Screener struct {
	entries   []entry
	threshold float64
	version   string
}

// New builds a Screener. threshold is the minimum similarity (0..1) that
// counts as a potential match.
func New(names []string, version string, threshold float64) *Screener {
	entries := make([]entry, 0, len(names))
	for _, n := range names {
		entries = append(entries, entry{original: n, normalized: Normalize(n)})
	}
	return &Screener{entries: entries, threshold: threshold, version: version}
}

// Screen returns the best match for name. Match is true when the best score
// reaches the threshold.
func (s *Screener) Screen(name string) Result {
	target := Normalize(name)
	best := Result{ListVersion: s.version}

	for _, e := range s.entries {
		score := similarity(target, e.normalized)
		if score > best.Score {
			best.Score = score
			best.MatchedName = e.original
		}
	}

	best.Score = math.Round(best.Score*1000) / 1000
	best.Match = best.Score >= s.threshold
	if !best.Match {
		best.MatchedName = ""
	}
	return best
}

var turkishToASCII = strings.NewReplacer(
	"İ", "I", "ı", "i",
	"Ş", "S", "ş", "s",
	"Ç", "C", "ç", "c",
	"Ğ", "G", "ğ", "g",
	"Ö", "O", "ö", "o",
	"Ü", "U", "ü", "u",
)

// Normalize makes two spellings of the same name comparable:
// Turkish letters become ASCII, everything is upper case, punctuation is
// removed and the name parts are sorted, so "Yılmaz, Ayşe" and
// "AYSE YILMAZ" both become "AYSE YILMAZ".
func Normalize(name string) string {
	name = strings.ToUpper(turkishToASCII.Replace(name))

	cleaned := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r
		}
		if unicode.IsSpace(r) || r == '-' || r == ',' || r == '.' || r == '\'' {
			return ' '
		}
		return -1 // drop anything else
	}, name)

	parts := strings.Fields(cleaned)
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// similarity returns 1 - (edit distance / length of the longer string).
func similarity(a, b string) float64 {
	if a == "" && b == "" {
		return 1
	}
	longest := max(len(a), len(b))
	return 1 - float64(levenshtein(a, b))/float64(longest)
}

// levenshtein counts the minimum number of single-character edits
// (insert, delete, replace) needed to turn a into b.
// Inputs are ASCII after Normalize, so indexing bytes is safe.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,      // delete
				curr[j-1]+1,    // insert
				prev[j-1]+cost, // replace
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
