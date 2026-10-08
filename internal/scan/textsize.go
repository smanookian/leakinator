package scan

import (
	"sort"
	"strings"

	"github.com/smanookian/leakinator/internal/ocr"
)

// Letter height means the height of tall letters (A-Z, 0-9, b, d, h, k, l…).
// OCR boxes fit the ink of a word, so a word's box height depends on its
// letters. These ratios turn any word box into a tall-letter height
// (measured on common monospace fonts).
const (
	ratioTallAndLow = 1.30 // "Type": tall letters and letters below the line
	ratioShortOnly  = 0.72 // "use": only short letters
	ratioShortLow   = 1.03 // "gray": short letters and letters below the line
	minWordConf     = 40
)

const (
	tall = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789bdfhklt"
	low  = "gjpqy"
)

// letterHeights returns one tall-letter height estimate per usable word.
// Only plain words (letters and digits) are used: brackets and quotes
// change the box height.
func letterHeights(words []ocr.Word) []float64 {
	var out []float64
	for _, w := range words {
		if w.Conf < minWordConf || len(w.Text) < 2 || !alnum(w.Text) {
			continue
		}
		h := float64(w.Box.Dy())
		hasTall := strings.ContainsAny(w.Text, tall)
		hasLow := strings.ContainsAny(w.Text, low)
		switch {
		case hasTall && hasLow:
			h /= ratioTallAndLow
		case !hasTall && hasLow:
			h /= ratioShortLow
		case !hasTall:
			h /= ratioShortOnly
		}
		out = append(out, h)
	}
	return out
}

func alnum(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
