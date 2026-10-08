// Package secretengine finds secrets in text, for example text read from a
// screen by OCR. It knows your real secrets (exact and partial matches that
// survive OCR mistakes) and common key formats.
//
// A Finding never holds the secret itself. Use Finding.Masked to show it.
//
//	secrets, _ := secretengine.LoadEnvFile(".env")
//	eng := secretengine.New(secretengine.Options{Secrets: secrets})
//	for _, f := range eng.Scan(line) { fmt.Println(f.Label, f.Masked) }
package secretengine

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Secret is a value that must not be shown, for example from a .env file.
type Secret struct {
	Name   string // variable name, e.g. "STRIPE_KEY"
	Value  string
	Source string // where it came from, e.g. "/home/me/app/.env"
}

// Kind says how a finding was made.
type Kind string

const (
	Exact   Kind = "exact"   // one of your secrets, all of it (OCR mistakes allowed)
	Partial Kind = "partial" // a long piece of one of your secrets (cut off or partly hidden)
	Pattern Kind = "pattern" // looks like a known key format
)

// Finding is one secret found in a text.
type Finding struct {
	Kind   Kind   `json:"kind"`
	Rule   string `json:"rule"`             // "env" or a pattern rule ID like "github-token"
	Label  string `json:"label"`            // short human name, e.g. "GitHub token" or "MY_TOKEN"
	Source string `json:"source,omitempty"` // file the secret came from (env findings)
	Masked string `json:"masked"`           // first 4 characters + "…"
	// Start and End are byte offsets of the match in the scanned text.
	Start int `json:"-"`
	End   int `json:"-"`
	// Matched is how many characters matched (env findings).
	Matched int `json:"matched,omitempty"`
	// Of is the length of the secret (env findings).
	Of int `json:"of,omitempty"`
}

// Options configure an Engine. Zero values use the defaults.
type Options struct {
	Secrets []Secret
	// MinPartial is the least number of characters in a row that must match
	// for a partial match. Default 12.
	MinPartial int
	// MaxErrorRate is the share of OCR mistakes allowed. Default 0.1 (1 in 10).
	MaxErrorRate float64
	// NoPatterns turns off the known key formats.
	NoPatterns bool
}

// Engine scans text. It is safe for use by many goroutines at once.
type Engine struct {
	secrets    []prepared
	minPartial int
	maxErr     float64
	patterns   []rule
}

type prepared struct {
	Secret
	folded []rune
	masked string
}

// New makes an Engine.
func New(o Options) *Engine {
	e := &Engine{minPartial: o.MinPartial, maxErr: o.MaxErrorRate}
	if e.minPartial <= 0 {
		e.minPartial = 12
	}
	if e.maxErr <= 0 {
		e.maxErr = 0.1
	}
	if !o.NoPatterns {
		e.patterns = rules
	}
	seen := map[string]bool{}
	for _, s := range o.Secrets {
		if seen[s.Value] {
			continue
		}
		seen[s.Value] = true
		e.secrets = append(e.secrets, prepared{Secret: s, folded: fold(s.Value), masked: Mask(s.Value)})
	}
	return e
}

// SecretCount is the number of distinct secrets the engine looks for.
func (e *Engine) SecretCount() int { return len(e.secrets) }

// Mask returns the first 4 characters of s and "…".
func Mask(s string) string {
	if utf8.RuneCountInString(s) <= 4 {
		return "…"
	}
	i := 0
	for range 4 {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i] + "…"
}

// Scan returns the secrets found in text. Findings that overlap are merged:
// your own secrets win over patterns, longer matches win over shorter ones.
func (e *Engine) Scan(text string) []Finding {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []Finding
	if len(e.secrets) > 0 {
		ft := foldText(text)
		for i := range e.secrets {
			if f, ok := e.matchSecret(&e.secrets[i], ft); ok {
				out = append(out, f)
			}
		}
	}
	// OCR often puts spaces inside keys ("ghp_JAVq lz4C"), so patterns
	// also run on the text without spaces.
	compact, offs := removeSpaces(text)
	for _, r := range e.patterns {
		out = append(out, r.find(text)...)
		if len(compact) < len(text) {
			for _, f := range r.find(compact) {
				f.Start, f.End = offs[f.Start], offs[f.End-1]+1
				out = append(out, f)
			}
		}
	}
	return dedupe(out)
}

// removeSpaces returns s without spaces and, for each byte of the result,
// its offset in s.
func removeSpaces(s string) (string, []int) {
	var b strings.Builder
	offs := make([]int, 0, len(s))
	for i := range len(s) {
		if c := s[i]; c != ' ' && c != '\t' {
			b.WriteByte(c)
			offs = append(offs, i)
		}
	}
	return b.String(), offs
}

func (e *Engine) matchSecret(s *prepared, ft foldedText) (Finding, bool) {
	n := len(s.folded)
	if len(ft.runes) == 0 {
		return Finding{}, false
	}
	f := Finding{Rule: "env", Label: s.Name, Source: s.Source, Masked: s.masked, Of: n}
	// All of the secret, with a few OCR mistakes?
	if edits, ts, te := within(s.folded, ft.runes); edits <= int(float64(n)*e.maxErr) && te > ts {
		f.Kind, f.Start, f.End, f.Matched = Exact, ft.starts[ts], ft.ends[te-1], n-edits
		return f, true
	}
	// A long piece of it (cut off at the screen edge, or partly covered)?
	a := align(s.folded, ft.runes)
	span := a.sEnd - a.sStart
	if n > e.minPartial && a.matches >= e.minPartial && a.errors <= int(float64(span)*e.maxErr) {
		f.Kind, f.Start, f.End, f.Matched = Partial, ft.starts[a.tStart], ft.ends[a.tEnd-1], a.matches
		return f, true
	}
	return Finding{}, false
}

func dedupe(fs []Finding) []Finding {
	if len(fs) < 2 {
		return fs
	}
	rank := func(f Finding) int {
		switch f.Kind {
		case Exact:
			return 0
		case Partial:
			return 1
		}
		return 2
	}
	sort.SliceStable(fs, func(i, j int) bool {
		if rank(fs[i]) != rank(fs[j]) {
			return rank(fs[i]) < rank(fs[j])
		}
		return fs[i].End-fs[i].Start > fs[j].End-fs[j].Start
	})
	var out []Finding
	for _, f := range fs {
		overlap := false
		for _, k := range out {
			if f.Start < k.End && k.Start < f.End {
				overlap = true
				break
			}
		}
		if !overlap {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
