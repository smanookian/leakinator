# Test videos

Made by `go run ./tools/mkfixtures`. All keys are fake. 1920x1080.

| File | What is in it |
|---|---|
| `env_key.mp4` | `MY_API_TOKEN` from `../test.env` shown 00:05-00:07. `DB_PASSWORD` cut off at the right edge 00:09-00:11. No audio. |
| `pattern_keys.mp4` | One fake key every 2 s from 00:01: OpenAI, Anthropic, GitHub, AWS, Stripe, Slack, JWT, private key. No audio. |
| `clean.mp4` | Code that mentions keys but shows none. Normal text size. Audio about -14 LUFS. |
| `small_text.mp4` | Normal text, then 13 px font 00:04-00:09, then normal again. No audio. |
| `loud_audio.mp4` | Loud noise, hard clipped 00:02-00:04, silence 00:06-00:12. |

The 20-minute speed test video is not committed (too big). Make it with
`go run ./tools/mkfixtures -long /tmp/long.mp4`.
