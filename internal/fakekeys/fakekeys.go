// Package fakekeys makes the fake keys shown in the test videos.
//
// Keys are built at run time from a fixed seed, so no key-like string
// sits in the source (GitHub push protection would block it).
package fakekeys

import (
	"encoding/base64"
	"math/rand/v2"
)

// Values from testdata/test.env. They match no known key pattern on purpose,
// so only the exact (.env) match can find them.
const (
	EnvToken    = "tk_9fQ2xZ7pLm4Rv9sK3wYb8Hc"
	EnvPassword = "Hunter2-Correct-Horse-91"
)

// Keys are fake keys in real key formats.
type Keys struct {
	OpenAI, Anthropic, GitHub, AWSID, AWSSecret, Stripe, Slack, JWT string
	PrivateKeyBody                                                  []string
}

// All returns every fake value, .env values included.
func (k Keys) All() []string {
	return append([]string{EnvToken, EnvPassword, k.OpenAI, k.Anthropic, k.GitHub, k.AWSID, k.AWSSecret, k.Stripe, k.Slack, k.JWT}, k.PrivateKeyBody...)
}

// New returns the same keys every time.
func New() Keys {
	r := rand.New(rand.NewPCG(42, 0))
	const b62 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	s := func(n int, set string) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = set[r.IntN(len(set))]
		}
		return string(b)
	}
	digits := "0123456789"
	b64 := b62 + "+/"
	var k Keys
	k.OpenAI = "sk-" + "proj-" + s(40, b62)
	k.Anthropic = "sk-" + "ant-api03-" + s(40, b62)
	k.GitHub = "gh" + "p_" + s(36, b62)
	k.AWSID = "AK" + "IA" + s(16, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
	k.AWSSecret = s(40, b64)
	k.Stripe = "sk_" + "live_" + s(24, b62)
	k.Slack = "xo" + "xb-" + s(12, digits) + "-" + s(13, digits) + "-" + s(24, b62)
	enc := base64.RawURLEncoding
	k.JWT = enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." +
		enc.EncodeToString([]byte(`{"sub":"42"}`)) + "." + s(43, b62)
	for range 3 {
		k.PrivateKeyBody = append(k.PrivateKeyBody, s(64, b64))
	}
	return k
}
