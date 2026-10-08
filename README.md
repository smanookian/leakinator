# Leakinator

Checks your coding video before you upload it. You give it a video. It gives
you a list of problems with timestamps:

- **Secrets on screen**: values from your `.env` files, and keys that look like
  OpenAI, Anthropic, GitHub, AWS, Stripe or Slack keys, JWTs and private keys.
  Cut-off keys and OCR mistakes are found too.
- **Text too small** to read on a phone.
- **Loudness** compared with YouTube (-14 LUFS), true peak, clipping and long silences.

Everything runs on your computer. No network, no cloud, no telemetry.
Secrets are never printed or saved: you only see the first 4 characters, like `sk-p…`.

```
$ leakinator video.mp4 --env .env
leakinator  video.mp4  (00:12, 1920x1080)

Secrets
  ✗ 00:05-00:07   MY_API_TOKEN from .env (tk_9…)
  ✗ 00:09-00:11   DB_PASSWORD from .env, partly visible (15 of 24 characters) (Hunt…)

Text size
  ✓ Text is big enough for a phone

Audio
  - No audio track

2 secrets found. Do not upload.  (checked in 0 s)
```

## Install

Leakinator needs `ffmpeg` and `tesseract` (with English).

**Arch / Omarchy** (one step, installs everything):

```
yay -S leakinator-bin
```

**macOS**: `brew install ffmpeg tesseract`, then download the `darwin` file from
[Releases](https://github.com/smanookian/leakinator/releases) and put `leakinator` in your PATH.

**Windows**: `winget install Gyan.FFmpeg UB-Mannheim.TesseractOCR`, then download
the `windows` zip from [Releases](https://github.com/smanookian/leakinator/releases).

**Other Linux**: install `ffmpeg` and `tesseract-ocr` (plus `tesseract-ocr-eng`)
with your package manager, then use the `linux` file from Releases.

**With Go**: `go install github.com/smanookian/leakinator/cmd/leakinator@latest`

## Commands

```
leakinator video.mp4                                  # all checks
leakinator video.mp4 --env .env --env ~/project/.env  # also look for your own secrets
leakinator video.mp4 --only secrets                   # only some checks: secrets, text, audio
leakinator video.mp4 --html report.html               # report with pictures (secrets pixelated)
leakinator video.mp4 --json                           # for scripts
```

More options: `--min-text PX` (default 14), `--fps N` (default 2), `--no-color`.
See `leakinator --help`.

**Exit code**: `0` no secrets, `1` secrets found, `2` error. Use it to stop an upload:

```
leakinator video.mp4 --env .env && upload video.mp4
```

## How it works

- **Frames**: looks at 2 frames per second, but reads text (OCR with Tesseract)
  only where the screen changed. A moving mouse or a blinking cursor does not
  cause new reads. A 20-minute 1080p video takes about 10-30 seconds.
- **Your secrets**: every value of 8 or more characters in the `.env` files
  (not numbers, not `true`/`false`). A match counts with up to 1 OCR mistake per
  10 characters. 12 or more characters in a row count as a partial match
  (for example a key cut off at the screen edge).
- **Text size**: the height of tall letters (A, b, d, 0-9…), scaled to 1080p.
  Below 14 px is hard to read on a 6" phone (14 px ≈ 1 mm on the phone). Change it with `--min-text`.
- **Loudness**: ffmpeg's `ebur128` filter (ITU BS.1770). Warns if louder than -13 or
  quieter than -16 LUFS, if true peak is above -1 dBTP, on clipping
  (3+ samples in a row at full scale) and on silence of 4 s or more below -50 dB.

Limits: OCR can miss text that is very small, blurry or low contrast. The HTML
report pixelates the secrets that were found; a secret OCR could not read is
not pixelated.

## JSON

`--json` prints one object: `secrets` (start, end, kind, rule, label, masked, box),
`small_text` (start, end, height_px), `audio` (lufs, true_peak, clipping,
silences, problems) and `summary` (secrets, warnings). Times are in seconds.

## Secret engine

The secret check is its own Go package, `secretengine`, for use in other apps:

```go
import "github.com/smanookian/leakinator/secretengine"

secrets, _ := secretengine.LoadEnvFile(".env")
eng := secretengine.New(secretengine.Options{Secrets: secrets})
for _, f := range eng.Scan(textFromScreen) {
	fmt.Println(f.Kind, f.Label, f.Masked, f.Start, f.End) // f never holds the secret
}
```

## Development

```
go test ./...                                   # video tests need ffmpeg and tesseract
go run ./tools/mkfixtures                       # remake the test videos in testdata/videos
go run ./tools/mkfixtures -long /tmp/long.mp4   # 20-minute speed test video
```

MIT license.
