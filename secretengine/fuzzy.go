package secretengine

import (
	"sync"
	"unicode"
	"unicode/utf8"
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
	case '_', '-', '—', '–':
		return '_'
	case '“', '”', '„':
		return '"'
	case '‘', '’':
		return '\''
	}
	return r
}

// fold folds s and drops spaces: OCR often adds or loses them.
func fold(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) {
			out = append(out, foldRune(r))
		}
	}
	return out
}

// foldedText is folded text without spaces. For each rune it keeps the
// byte offsets in the original text where the rune starts and ends.
type foldedText struct {
	runes        []rune
	starts, ends []int
}

func foldText(s string) foldedText {
	ft := foldedText{runes: make([]rune, 0, len(s)), starts: make([]int, 0, len(s)), ends: make([]int, 0, len(s))}
	for i, r := range s {
		if !unicode.IsSpace(r) {
			ft.runes = append(ft.runes, foldRune(r))
			ft.starts = append(ft.starts, i)
			ft.ends = append(ft.ends, i+utf8.RuneLen(r))
		}
	}
	return ft
}

// within finds all of s inside t with the fewest edits (insert, delete,
// change one character). It returns the edits and where in t it matched.
func within(s, t []rune) (edits, tStart, tEnd int) {
	n, m := len(s), len(t)
	if n == 0 || m == 0 {
		return n, 0, 0
	}
	w := m + 1
	bp := scorePool.Get().(*[]int32)
	defer scorePool.Put(bp)
	need := (n + 1) * w
	if cap(*bp) < need {
		*bp = make([]int32, need)
	}
	D := (*bp)[:need]
	for j := range w {
		D[j] = 0 // the match may start anywhere in t
	}
	for i := 1; i <= n; i++ {
		row, prev := D[i*w:], D[(i-1)*w:]
		row[0] = int32(i)
		for j := 1; j <= m; j++ {
			c := prev[j-1]
			if s[i-1] != t[j-1] {
				c++
			}
			row[j] = min(c, prev[j]+1, row[j-1]+1)
		}
	}
	last := D[n*w:]
	tEnd = 1
	for j := 2; j <= m; j++ {
		if last[j] < last[tEnd] {
			tEnd = j
		}
	}
	// Walk back to find where the match starts.
	i, j := n, tEnd
	for i > 0 && j > 0 {
		v := D[i*w+j]
		c := D[(i-1)*w+j-1]
		if s[i-1] != t[j-1] {
			c++
		}
		switch {
		case v == c:
			i, j = i-1, j-1
		case v == D[(i-1)*w+j]+1:
			i--
		default:
			j--
		}
	}
	return int(last[tEnd]), j, tEnd
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
