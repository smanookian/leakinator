package secretengine

import (
	"encoding/json"
	"strings"
	"testing"
)

// Key prefixes are split so secret scanners don't flag this file.
const (
	envToken = "tk_9fQ2xZ7pLm4Rv9sK3wYb8Hc"
	envPass  = "Hunter2-Correct-Horse-91"
)

func engine() *Engine {
	return New(Options{Secrets: []Secret{
		{Name: "MY_API_TOKEN", Value: envToken, Source: "test.env"},
		{Name: "DB_PASSWORD", Value: envPass, Source: "test.env"},
	}})
}

func one(t *testing.T, fs []Finding) Finding {
	t.Helper()
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(fs), fs)
	}
	return fs[0]
}

func TestEnvMatch(t *testing.T) {
	e := engine()
	tests := []struct {
		name, text string
		kind       Kind // "" = no finding
		label      string
	}{
		{"exact", "MY_API_TOKEN=" + envToken, Exact, "MY_API_TOKEN"},
		// OCR look-alikes (0/O, 1/l, q/g) are not counted as mistakes.
		{"look-alikes", "MY_API_TOKEN=tk_9fg2xZ7pLm4Rv9sK3wYb8Hc", Exact, "MY_API_TOKEN"},
		// 2 real mistakes in 26 characters are allowed.
		{"two mistakes", "token tk_9fQ2xZ7pLm4Rv9sK3wYbBHe", Exact, "MY_API_TOKEN"},
		{"too many mistakes", "tk_9eQ2xZ7pAm4Rw9sK3wYxBHe", "", ""},
		{"spaces from OCR", "tk 9fQ2xZ7pLm4Rv9sK3wYb8Hc", Exact, "MY_API_TOKEN"},
		// Cut off at the screen edge: 15 of 24 characters.
		{"cut off", "db password: Hunter2-Correct", Partial, "DB_PASSWORD"},
		{"12 chars is enough", "pw Hunter2-Corr", Partial, "DB_PASSWORD"},
		{"11 chars is not", "pw Hunter2-Cor", "", ""},
		{"middle part", "xx2-Correct-Horsexx", Partial, "DB_PASSWORD"},
		{"unrelated", "func main() { fmt.Println(\"hello\") }", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := e.Scan(tt.text)
			if tt.kind == "" {
				if len(fs) != 0 {
					t.Fatalf("want nothing, got %+v", fs)
				}
				return
			}
			f := one(t, fs)
			if f.Kind != tt.kind || f.Label != tt.label || f.Rule != "env" {
				t.Fatalf("got %+v", f)
			}
		})
	}
}

func TestEnvMatchOffsets(t *testing.T) {
	text := "MY_API_TOKEN=" + envToken + " # comment"
	f := one(t, engine().Scan(text))
	if got := text[f.Start:f.End]; got != envToken {
		t.Fatalf("span = %q", got)
	}
}

func TestShortSecretNeedsFullMatch(t *testing.T) {
	e := New(Options{Secrets: []Secret{{Name: "PIN", Value: "x7Kq92mZ"}}})
	if fs := e.Scan("pin: x7Kq92mZ"); len(fs) != 1 || fs[0].Kind != Exact {
		t.Fatalf("got %+v", fs)
	}
	if fs := e.Scan("pin: x7Kq92mA"); len(fs) != 0 {
		t.Fatalf("8-char secret allows no mistakes, got %+v", fs)
	}
}

func TestPatterns(t *testing.T) {
	e := New(Options{})
	// Real Tesseract output from the test videos, OCR mistakes included.
	tests := []struct{ rule, text, masked string }{
		{"openai-key", "OPENAI_API_KEY=sk-" + "proj-17IFL75Wo7h10PgkfNel1tAQ3mmmUkf2nZqg0OTEq", "sk-p…"},
		{"anthropic-key", "ANTHROPIC_API_KEY=sk-" + "ant-api03-Xq3Lp0V9mWk2RtY7uZbN4sGfHd1Je8CoAi5", "sk-a…"},
		{"github-token", "git remote set-url origin https://gh" + "p_JAVqlz4CE80jy62uL4BSTNLmYYQwOvfIkIZI@github.com/me/app", "ghp_…"},
		{"github-token", "token: github" + "_pat_11ABCDEFG0123456789_abcdefghijKLMNOP", "gith…"},
		{"aws-access-key", "aws_access_key_id = AK" + "IADKEEN2LQYJV7L53A", "AKIA…"},
		{"aws-secret-key", "aws_secret_access_key = TmNhmDk+" + "c4xvtPqpcxJLsiPfCnaezAJzeTuEGjQd", "TmNh…"},
		// OCR turned "_" into a space.
		{"stripe-key", `stripe.api_key = "sk_` + `live ubEz1K0gDnT4hdBIa3cORnge"`, "sk_l…"},
		// OCR added a space after "_".
		{"stripe-key", `stripe.api_key = "sk_` + `live_ ubEz1K0gDnT4hdBIa3cORnge"`, "sk_l…"},
		// OCR read "xoxb" as "x0xb".
		{"slack-token", "SLACK_BOT_TOKEN=x0x" + "b-716831189025-2170189170554-9tfhvQJuaGruDs93E7jLAhSn", "x0xb…"},
		{"slack-webhook", "url = https://hooks.slack.com/services/" + "T0000000/B0000000/XXXXXXXXXXXXXXXX", "hook…"},
		// Cut off at the screen edge, OCR mistakes inside.
		{"jwt", "Authorization: Bearer eyJhbGci0iJIUzIINiIsInR5cCI6IkpXVCJ9.eyJzdWIi0iIOMiJ9.mtfYSsGnDiQKbBQgzullaGlwstSxjYvBq", "eyJh…"},
		{"private-key", "-----BEGIN RSA PRIVATE KEY-----", "BEGI…"},
		{"private-key", "-----BEGIN OPENSSH PRIVATE KEY-----", "BEGI…"},
		// Tesseract's "fast" model (Ubuntu) puts spaces inside keys.
		{"openai-key", "OPENAI_API_KEY=sk- proj - 17IFL75Wo7hlLOPqkfNellLtAQ3mmmUk f2nZqgOTEq", "sk-p…"},
		{"github-token", "git remote set-url origin https://gh" + "p_JAVqlz4CE80j y62uL4BS fNLmYYQwOvfIkIZI@github.com/me/app", "ghp_…"},
		{"slack-token", "SLACK_BOT_TOKEN=xo" + "xb - 716831189025 - 2170189170554 - 9t fhvQJuaGruDs93E7jLAhSn", "xoxb…"},
		// The shown characters never include OCR's extra spaces.
		{"anthropic-key", "ANTHROPIC_API_KEY=sk- " + "ant-api03-Xq3Lp0V9mWk2RtY7uZbN4sGfHd1Je8CoAi5", "sk-a…"},
	}
	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			f := one(t, e.Scan(tt.text))
			if f.Kind != Pattern || f.Rule != tt.rule || f.Masked != tt.masked {
				t.Fatalf("got %+v", f)
			}
		})
	}
}

// Lines from a normal coding video must not raise alarms.
func TestNoFalseAlarms(t *testing.T) {
	e := New(Options{Secrets: []Secret{{Name: "DB_PASSWORD", Value: envPass}}})
	clean := []string{
		"const openai = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });",
		`const stripe = require("stripe")(process.env.STRIPE_SECRET_KEY);`,
		"// GitHub tokens start with gh" + "p_ and Stripe keys with sk_" + "live_",
		`const id = "3f2c9a1e-7b4d-4e8a-9c1f-2d5e6b7a8c90"; // request id`,
		"// commit 9fceb02d0ae598e95dc970b74767f19372d61af8",
		`<button class="sk-button-primary-large-variant">`,
		"const sha = createHash(\"sha256\").update(body).digest(\"hex\");",
		"export AWS_REGION=eu-central-1",
		"password: ********",
		"The bridge weighs about Ten thousand tons since 2024 and laughs at storms",
		"curl -H \"Authorization: Bearer $TOKEN\" https://api.example.com/v1/items",
		"",
	}
	for _, line := range clean {
		if fs := e.Scan(line); len(fs) != 0 {
			t.Errorf("false alarm on %q: %+v", line, fs)
		}
	}
}

// Your own secret wins over a pattern match on the same text.
func TestEnvWinsOverPattern(t *testing.T) {
	key := "gh" + "p_JAVqlz4CE8Ojy62uL4BSfNLmYYQwOvfIkIZI"
	e := New(Options{Secrets: []Secret{{Name: "GH_TOKEN", Value: key}}})
	f := one(t, e.Scan("export GH_TOKEN="+key))
	if f.Rule != "env" || f.Label != "GH_TOKEN" {
		t.Fatalf("got %+v", f)
	}
}

// Findings, also as JSON, never hold more than the first 4 characters.
func TestFindingsNeverHoldSecret(t *testing.T) {
	e := engine()
	fs := e.Scan("MY_API_TOKEN=" + envToken + " DB=" + envPass + " sk_" + "live_ubEz1K0gDnT4hdBIa3cORnge")
	if len(fs) != 3 {
		t.Fatalf("want 3 findings, got %+v", fs)
	}
	b, _ := json.Marshal(fs)
	for _, s := range []string{envToken, envPass, "ubEz1K0gDnT4hdBIa3cORnge"} {
		if strings.Contains(string(b), s[4:9]) {
			t.Fatalf("JSON holds part of a secret beyond the first 4 chars: %s", b)
		}
	}
}

// OCR reading the first characters a bit differently is still the same key.
func TestKeySurvivesLookAlikes(t *testing.T) {
	e := New(Options{})
	a := one(t, e.Scan("SLACK_BOT_TOKEN=x0x"+"b-716831189025-2170189170554-9tfhvQJuaGruDs93E7jLAhSn"))
	b := one(t, e.Scan("SLACK_BOT_TOKEN=xox"+"b-716831189025-2170189170554-9tfhvQJuaGruDs93E7jLAhSn"))
	c := one(t, e.Scan("token = gh"+"p_JAVqlz4CE8Ojy62uL4BSfNLmYYQwOvfIkIZI"))
	if a.Key() != b.Key() {
		t.Fatalf("%q and %q differ", a.Masked, b.Masked)
	}
	if a.Key() == c.Key() {
		t.Fatal("different rules share a key")
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"abcdefgh": "abcd…", "äöüßxyz": "äöüß…", "abcd": "…", "": "…"} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseEnv(t *testing.T) {
	in := `# comment
export API_KEY=abc123def456
QUOTED="with \"quotes\" inside"
SINGLE='single quoted value'
INLINE=plainvalue123 # trailing comment
PEM="-----BEGIN KEY-----
line2line2line2
-----END KEY-----"
PORT=3000
TIMEOUT=1234567890.5
DEBUG=true
SHORT=abc
EMPTY=
NOEQUALS
`
	got, err := ParseEnv(strings.NewReader(in), "x.env")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"API_KEY": "abc123def456",
		"QUOTED":  `with "quotes" inside`,
		"SINGLE":  "single quoted value",
		"INLINE":  "plainvalue123",
		"PEM":     "-----BEGIN KEY-----\nline2line2line2\n-----END KEY-----",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d secrets: %+v", len(got), got)
	}
	for _, s := range got {
		if want[s.Name] != s.Value || s.Source != "x.env" {
			t.Errorf("%s = %q", s.Name, s.Value)
		}
	}
}

func TestParseEnvUnclosedQuoteHidesValue(t *testing.T) {
	_, err := ParseEnv(strings.NewReader(`KEY="supersecretvalue`), "x.env")
	if err == nil || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("err = %v", err)
	}
}
