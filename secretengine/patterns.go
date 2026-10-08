package secretengine

import (
	"regexp"
	"strings"
	"unicode"
)

// Rule describes one known key format.
type Rule struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type rule struct {
	Rule
	re *regexp.Regexp
	// group is the regexp group that holds the key (0 = whole match).
	group int
	// random, if set, must accept the key part (filters out normal words).
	random func(string) bool
}

// Rules lists the known key formats, in the order they are tried.
func Rules() []Rule {
	out := make([]Rule, len(rules))
	for i, r := range rules {
		out[i] = r.Rule
	}
	return out
}

func (r rule) find(text string) []Finding {
	var out []Finding
	for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2*r.group], m[2*r.group+1]
		if start < 0 {
			continue
		}
		key := text[start:end]
		if r.random != nil && !r.random(key) {
			continue
		}
		masked, _ := removeSpaces(key)
		out = append(out, Finding{Kind: Pattern, Rule: r.ID, Label: r.Label, Masked: Mask(masked), Start: start, End: end})
	}
	return out
}

// lit turns a literal key prefix into a regexp that allows common OCR
// mistakes: O/0, l/1/I, S/5, and "_", "-" or " " for each other.
func lit(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch unicode.ToLower(r) {
		case 'o', '0':
			b.WriteString(`[oO0]`)
		case 'l', 'i', '1':
			b.WriteString(`[lI1i|]`)
		case 's', '5':
			b.WriteString(`[sS5]`)
		case 'g', 'q':
			b.WriteString(`[gq9]`)
		case '_', '-':
			b.WriteString(`(?:[-_] ?| )`) // OCR may add a space or read "_" as one
		default:
			if unicode.IsLetter(r) {
				b.WriteString(`[` + string(unicode.ToLower(r)) + string(unicode.ToUpper(r)) + `]`)
			} else {
				b.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
	}
	return b.String()
}

// mixed accepts strings that look random: they have a digit, or both
// upper and lower case letters. "button-primary-large" is rejected.
func mixed(s string) bool {
	var up, lo, dig bool
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			lo = true
		case unicode.IsDigit(r):
			dig = true
		}
	}
	return dig || (up && lo)
}

const (
	nb = `(?:^|[^A-Za-z0-9])` // no letter or digit before
	na = `(?:[^A-Za-z0-9]|$)` // no letter or digit after
)

// Order matters: earlier rules win when matches overlap
// (sk-ant- is Anthropic, not OpenAI; sk_live_ is Stripe).
// Distinctive prefixes (ghp_, xoxb-, eyJ…) need no word boundary before
// them: in text without spaces a word can run into them.
var rules = []rule{
	{Rule: Rule{"private-key", "Private key"},
		re: regexp.MustCompile(`(?i)BEGIN\s*(?:[A-Z0-9]+\s*){0,3}?PRIVATE\s*KEY`)},
	{Rule: Rule{"anthropic-key", "Anthropic API key"},
		re: regexp.MustCompile(`(` + lit("sk-ant-") + `[A-Za-z0-9_\-]{16,})`), group: 1, random: mixed},
	{Rule: Rule{"stripe-key", "Stripe secret key"},
		re: regexp.MustCompile(`((?:` + lit("sk_live_") + `|` + lit("sk_test_") + `|` + lit("rk_live_") + `|` + lit("rk_test_") + `)[A-Za-z0-9]{16,})`), group: 1, random: mixed},
	{Rule: Rule{"openai-key", "OpenAI API key"},
		re: regexp.MustCompile(nb + `(` + lit("sk-") + `(?:` + lit("proj-") + `|` + lit("svcacct-") + `|` + lit("admin-") + `)?[A-Za-z0-9_\-]{20,})`), group: 1, random: mixed},
	{Rule: Rule{"github-token", "GitHub token"},
		re: regexp.MustCompile(`((?:` + lit("gh") + `[pousrPOUSR]` + lit("_") + `[A-Za-z0-9]{20,})|(?:` + lit("github_pat_") + `[A-Za-z0-9_]{20,}))`), group: 1, random: mixed},
	{Rule: Rule{"aws-access-key", "AWS access key ID"},
		re: regexp.MustCompile(nb + `((?:A[K][lI1|]A|AS[lI1|]A|ABIA|ACCA)[A-Z0-9]{12,16})` + na), group: 1},
	{Rule: Rule{"aws-secret-key", "AWS secret access key"},
		re: regexp.MustCompile(`(?i)aws.{0,24}?(?:secret|sk).{0,24}?[=:]\s*["']?([A-Za-z0-9/+]{30,40})` + na), group: 1, random: mixed},
	{Rule: Rule{"slack-token", "Slack token"},
		re: regexp.MustCompile(`(` + lit("xox") + `[baprseBAPRSE]` + lit("-") + `[A-Za-z0-9\-]{10,})`), group: 1, random: mixed},
	{Rule: Rule{"slack-webhook", "Slack webhook URL"},
		re: regexp.MustCompile(`hooks\.slack\.com/services/[A-Za-z0-9/]{16,}`)},
	{Rule: Rule{"jwt", "JWT (login token)"},
		re: regexp.MustCompile(`([eE][yY][jJ][A-Za-z0-9_\-]{8,}\.[eE][yY][jJ][A-Za-z0-9_\-]{4,}(?:\.[A-Za-z0-9_\-]*)?)`), group: 1},
}
