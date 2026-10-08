package secretengine

import (
	"sync"
	"unicode"
)

// fold maps characters that OCR often mixes up to one form,
// so "0" and "O", or "l", "1" and "I", compare equal.
func foldRune(r rune) rune {
	r = unicode.ToLower(r)
	switch r {
	case '0', 'o':
		return 'o'
	case '1', 'l', 'i', '|', '!':
		return 'l'
	case '5', 's':
		return 's'
	case '8', 'b':
		return 'b'
	case '9', 'g', 'q':
		return 'g'
	case '2', 'z':
		return 'z'
	case 'f', 't':
		return 't'
	case '_', '-', ' ', '\t', '—', '–':
		return '_'
	case '“', '”', '„':
		return '"'
	case '‘', '’':
		return '\''
	}
	return r
}

func fold(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		out = append(out, foldRune(r))
	}
	return out
}

// foldedText is folded text plus the byte offset of each rune in the
// original text. offsets has one extra entry: len(text).
type foldedText struct {
	runes   []rune
	offsets []int
}

func foldText(s string) foldedText {
	ft := foldedText{runes: make([]rune, 0, len(s)), offsets: make([]int, 0, len(s)+1)}
	for i, r := range s {
		ft.runes = append(ft.runes, foldRune(r))
		ft.offsets = append(ft.offsets, i)
	}
	ft.offsets = append(ft.offsets, len(s))
	return ft
}

// alignment is the best local match of a secret s inside a text t.
// s[sStart:sEnd] lines up with t[tStart:tEnd].
type alignment struct {
	sStart, sEnd, tStart, tEnd int
	matches, errors            int
}

const (
	scoreMatch    = 1
	scoreMismatch = -2
	scoreGap      = -2
)

var scorePool = sync.Pool{New: func() any { b := make([]int32, 0, 4096); return &b }}

// align finds the best local alignment (Smith-Waterman) of s in t.
func align(s, t []rune) alignment {
	n, m := len(s), len(t)
	if n == 0 || m == 0 {
		return alignment{}
	}
	w := m + 1
	bp := scorePool.Get().(*[]int32)
	defer scorePool.Put(bp)
	need := (n + 1) * w
	if cap(*bp) < need {
		*bp = make([]int32, need)
	}
	H := (*bp)[:need]
	for j := range w {
		H[j] = 0
	}
	var best int32
	bi, bj := 0, 0
	for i := 1; i <= n; i++ {
		row, prev := H[i*w:], H[(i-1)*w:]
		row[0] = 0
		for j := 1; j <= m; j++ {
			d := prev[j-1] + scoreMismatch
			if s[i-1] == t[j-1] {
				d = prev[j-1] + scoreMatch
			}
			v := max(0, d, prev[j]+scoreGap, row[j-1]+scoreGap)
			row[j] = v
			if v > best {
				best, bi, bj = v, i, j
			}
		}
	}
	if best == 0 {
		return alignment{}
	}
	a := alignment{sEnd: bi, tEnd: bj}
	i, j := bi, bj
	for i > 0 && j > 0 && H[i*w+j] > 0 {
		v := H[i*w+j]
		switch {
		case s[i-1] == t[j-1] && v == H[(i-1)*w+j-1]+scoreMatch:
			a.matches++
			i, j = i-1, j-1
		case s[i-1] != t[j-1] && v == H[(i-1)*w+j-1]+scoreMismatch:
			a.errors++
			i, j = i-1, j-1
		case v == H[(i-1)*w+j]+scoreGap:
			a.errors++
			i--
		default:
			a.errors++
			j--
		}
	}
	a.sStart, a.tStart = i, j
	return a
}
