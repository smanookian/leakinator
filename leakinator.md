# Leakinator (Video Preflight)

Checks your coding video before you upload it. Research: `research/pm-content.md` (Video Preflight).
Status: planned (2026-10-08).

## 1. What it is
- A terminal tool (CLI). Linux first (Omarchy), also macOS and Windows.
- You give it a video file. It gives you a list of problems with timestamps.
- Free, MIT. Its secret engine is reused later by Secret Shield (paid).

## 2. Why
- 28.6 million secrets leaked on public GitHub in 2025 (+34%). AI keys +81%. (GitGuardian 2026)
- Live-masking tools exist, but none checks the finished video.
- No tool found that checks if code is readable on a phone.

## 3. Checks (v1)
### 3.1 Secrets
- Reads text from video frames (OCR).
- Two ways to find a secret:
  1. **Exact match** with your real secrets: values from `.env` files you point it to. Best signal, few false alarms.
  2. **Known patterns**: OpenAI, Anthropic, GitHub, AWS, Stripe, Slack keys, JWTs, private key headers.
- Partial matches count too (OCR can miss a letter, a key can be cut off at the screen edge).
- Your secrets never leave the machine and are never written to the report. The report shows only the first 4 characters + `…`.

### 3.2 Code too small
- Finds code/terminal text in frames and measures letter height.
- Warns if text would be too small on a phone (default: below a set size at 1080p scaled to a 6" screen).
- Groups by scene: "02:10-03:45 text too small (about 9 px tall)".

### 3.3 Loudness
- Measures loudness (LUFS, ITU BS.1770) and true peak.
- Compares with YouTube's target (-14 LUFS). Flags clipping and long silences.

## 4. Speed
- Does not OCR every frame. Samples frames and only re-reads when the screen changes (scene/diff detection).
- Goal: a 20-minute 1080p video checked in under 5 minutes on a normal laptop CPU.

## 5. Output
- Terminal: short list, grouped by check, with timestamps. Red / yellow / green.
- `--json` for scripts and for later apps.
- `--html` report with frame thumbnails (secrets blurred in the thumbnails).
- Exit code not 0 if a secret is found, so it can block an upload script.

## 6. Commands
- `leakinator video.mp4`
- `leakinator video.mp4 --env .env --env ~/project/.env`
- `leakinator video.mp4 --only secrets`
- `leakinator video.mp4 --html report.html`

## 7. Tech (proposal, the builder can push back)
- Language: Go or Rust (one binary, easy to ship on 3 OSes). Python only if OCR quality needs it.
- Video and audio: `ffmpeg` (frames, loudness via `ebur128` filter).
- OCR: Tesseract or a small local OCR model. No cloud.
- Secret engine as its own module with a clean API, so Secret Shield can reuse it.

## 8. Not in v1
Auto-blur export. Desktop app. Live screen masking (that is Secret Shield). Checking screenshots/images (easy later).

## 9. Done when
1. Test video with a fake key from a test `.env` shown for 2 seconds → found, correct timestamp.
2. Test video with known-pattern fake keys (OpenAI, GitHub, AWS) → all found.
3. A clean video → no secret alarms.
4. Small-text test video → flagged with the right time range.
5. Loudness matches `ffmpeg ebur128` within 0.5 LU.
6. Real secrets never appear in output, logs or report.
7. 20-minute 1080p video in under 5 minutes.
8. Install on Omarchy in one step (AUR or a single binary). Also builds for macOS and Windows in CI.

## 10. Open questions
- Name: "Leakinator" chosen by Steve 2026-10-08. Checked: GitHub 0 repos, AUR 0. App stores not checked.
- Text-size limit for phones: pick the exact number in the build (test on a real phone).
