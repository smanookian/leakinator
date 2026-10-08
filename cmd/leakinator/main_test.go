package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	stdhtml "html"
	"image"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/smanookian/leakinator/internal/fakekeys"
	"github.com/smanookian/leakinator/internal/ocr"
	"github.com/smanookian/leakinator/internal/report"
	"github.com/smanookian/leakinator/secretengine"
)

const env = "../../testdata/test.env"

func video(t *testing.T, name string) string {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe", "tesseract"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	p := filepath.Join("../../testdata/videos", name)
	// Stat makes `go test` caching notice changes to the video.
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

type out struct {
	code           int
	stdout, stderr string
}

func runArgs(args ...string) out {
	var so, se bytes.Buffer
	code := run(args, &so, &se)
	return out{code, so.String(), se.String()}
}

func runJSON(t *testing.T, args ...string) (*report.Report, int) {
	t.Helper()
	o := runArgs(append(args, "--json")...)
	if o.code == exitError {
		t.Fatalf("exit %d: %s", o.code, o.stderr)
	}
	var r report.Report
	if err := json.Unmarshal([]byte(o.stdout), &r); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, o.stdout)
	}
	return &r, o.code
}

func near(a, b float64) bool { return math.Abs(a-b) <= 0.5 }

func find(r *report.Report, rule, label string) *report.Secret {
	for i, s := range r.Secrets {
		if s.Rule == rule && (label == "" || s.Label == label) {
			return &r.Secrets[i]
		}
	}
	return nil
}

// Done item 1: a key from a test .env shown for 2 seconds.
func TestEnvKeyFound(t *testing.T) {
	r, code := runJSON(t, video(t, "env_key.mp4"), "--env", env, "--only", "secrets")
	if code != exitSecret {
		t.Fatalf("exit code %d, want %d", code, exitSecret)
	}
	s := find(r, "env", "MY_API_TOKEN")
	if s == nil || s.Kind != "exact" || !near(s.Start, 5) || !near(s.End, 7) {
		t.Fatalf("MY_API_TOKEN: got %+v, want exact 5-7 s", s)
	}
	// Cut off at the screen edge.
	p := find(r, "env", "DB_PASSWORD")
	if p == nil || p.Kind != "partial" || !near(p.Start, 9) || !near(p.End, 11) {
		t.Fatalf("DB_PASSWORD: got %+v, want partial 9-11 s", p)
	}
	if len(r.Secrets) != 2 {
		t.Fatalf("want 2 secrets, got %+v", r.Secrets)
	}
}

// Without --env, the .env value (no known format) is not found.
func TestEnvKeyNeedsEnvFile(t *testing.T) {
	r, code := runJSON(t, video(t, "env_key.mp4"), "--only", "secrets")
	if code != exitOK || len(r.Secrets) != 0 {
		t.Fatalf("exit %d, secrets %+v", code, r.Secrets)
	}
}

// Done item 2: known key formats.
func TestPatternKeysFound(t *testing.T) {
	r, code := runJSON(t, video(t, "pattern_keys.mp4"), "--only", "secrets")
	if code != exitSecret {
		t.Fatalf("exit code %d", code)
	}
	// Each key is on screen for 2 s, one after the other from 1 s.
	want := []struct {
		rule  string
		start float64
	}{
		{"openai-key", 1}, {"anthropic-key", 3}, {"github-token", 5}, {"aws-access-key", 7},
		{"aws-secret-key", 7}, {"stripe-key", 9}, {"slack-token", 11}, {"jwt", 13}, {"private-key", 15},
	}
	for _, w := range want {
		s := find(r, w.rule, "")
		if s == nil || !near(s.Start, w.start) || !near(s.End, w.start+2) {
			t.Errorf("%s: got %+v, want %g-%g s", w.rule, s, w.start, w.start+2)
		}
	}
	if len(r.Secrets) != len(want) {
		t.Errorf("want %d secrets, got %d: %+v", len(want), len(r.Secrets), r.Secrets)
	}
}

// Done item 3: no false alarms on a clean video.
func TestCleanVideo(t *testing.T) {
	r, code := runJSON(t, video(t, "clean.mp4"), "--env", env)
	if code != exitOK || len(r.Secrets) != 0 || len(r.SmallText) != 0 || r.Summary.Warnings != 0 {
		t.Fatalf("exit %d, report %+v", code, r)
	}
}

// Done item 4: small text with the right time range.
func TestSmallText(t *testing.T) {
	r, code := runJSON(t, video(t, "small_text.mp4"), "--only", "text")
	if code != exitOK {
		t.Fatalf("exit code %d", code)
	}
	if len(r.SmallText) != 1 {
		t.Fatalf("want 1 range, got %+v", r.SmallText)
	}
	s := r.SmallText[0]
	if !near(s.Start, 4) || !near(s.End, 9) || s.HeightPx >= 14 || s.HeightPx < 8 {
		t.Fatalf("got %+v, want 4-9 s, height 8-14 px", s)
	}
	// A lower limit lets the same text pass.
	r, _ = runJSON(t, video(t, "small_text.mp4"), "--only", "text", "--min-text", "8")
	if len(r.SmallText) != 0 {
		t.Fatalf("--min-text 8: got %+v", r.SmallText)
	}
}

// ebur128 measures loudness the way ffmpeg does on its own.
func ffmpegLUFS(t *testing.T, path string) (lufs, peak float64) {
	out, err := exec.Command("ffmpeg", "-nostats", "-hide_banner", "-i", path, "-map", "0:a:0",
		"-af", "ebur128=peak=true", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	get := func(re string) float64 {
		m := regexp.MustCompile(re).FindAllSubmatch(out, -1)
		if len(m) == 0 {
			t.Fatalf("no %s in ffmpeg output", re)
		}
		v, _ := strconv.ParseFloat(string(m[len(m)-1][1]), 64)
		return v
	}
	return get(`I:\s+(-?[\d.]+) LUFS`), get(`Peak:\s+(-?[\d.]+) dBFS`)
}

// Done item 5: loudness matches ffmpeg ebur128 within 0.5 LU.
func TestLoudness(t *testing.T) {
	for _, name := range []string{"clean.mp4", "loud_audio.mp4"} {
		path := video(t, name)
		r, _ := runJSON(t, path, "--only", "audio")
		lufs, peak := ffmpegLUFS(t, path)
		if r.Audio == nil || math.Abs(r.Audio.LUFS-lufs) > 0.5 || math.Abs(r.Audio.TruePeak-peak) > 0.5 {
			t.Fatalf("%s: got %+v, ffmpeg says %.1f LUFS, %.1f dBTP", name, r.Audio, lufs, peak)
		}
	}
	r, _ := runJSON(t, video(t, "clean.mp4"), "--only", "audio")
	if len(r.Audio.Problems) != 0 {
		t.Errorf("clean audio has problems: %v", r.Audio.Problems)
	}
	r, _ = runJSON(t, video(t, "loud_audio.mp4"), "--only", "audio")
	a := r.Audio
	if a.LUFS <= -13 || a.TruePeak <= -1 {
		t.Errorf("loud video not loud: %+v", a)
	}
	if len(a.Clipping) != 1 || !near(a.Clipping[0].Start, 2) || !near(a.Clipping[0].End, 4) {
		t.Errorf("clipping: got %+v, want 2-4 s", a.Clipping)
	}
	if len(a.Silences) != 1 || !near(a.Silences[0].Start, 6) || !near(a.Silences[0].End, 12) {
		t.Errorf("silence: got %+v, want 6-12 s", a.Silences)
	}
	if r, _ := runJSON(t, video(t, "env_key.mp4"), "--only", "audio"); !r.NoAudio || r.Audio != nil {
		t.Errorf("video without audio: %+v", r)
	}
}

// Done item 6: secrets never appear in the output, the errors or the report.
func TestSecretsNeverShown(t *testing.T) {
	secrets := fakekeys.New().All()
	html := filepath.Join(t.TempDir(), "r.html")
	var all strings.Builder
	for _, name := range []string{"env_key.mp4", "pattern_keys.mp4"} {
		v := video(t, name)
		for _, args := range [][]string{
			{v, "--env", env},
			{v, "--env", env, "--json"},
			{v, "--env", env, "--html", html},
		} {
			o := runArgs(args...)
			if o.code != exitSecret {
				t.Fatalf("%v: exit %d: %s", args, o.code, o.stderr)
			}
			all.WriteString(o.stdout + o.stderr)
		}
		b, err := os.ReadFile(html)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(b)
		checkThumbs(t, string(b))
	}
	text := all.String()
	for _, s := range secrets {
		// Only the first 4 characters may be shown: no 8 characters in a row.
		for i := 0; i+8 <= len(s); i++ {
			if strings.Contains(text, s[i:i+8]) {
				t.Fatalf("output holds part of a secret: %q…", s[:4])
			}
		}
	}
}

// checkThumbs reads the HTML thumbnails with OCR (enlarged, so OCR has
// the best chance). Our own engine must find no secret in them.
func checkThumbs(t *testing.T, html string) {
	t.Helper()
	sec, err := secretengine.LoadEnvFile(env)
	if err != nil {
		t.Fatal(err)
	}
	eng := secretengine.New(secretengine.Options{Secrets: sec})
	r, err := ocr.New()
	if err != nil {
		t.Fatal(err)
	}
	thumbs := regexp.MustCompile(`data:image/jpeg;base64,([A-Za-z0-9+/=]+)`).FindAllStringSubmatch(stdhtml.UnescapeString(html), -1)
	if len(thumbs) == 0 {
		t.Fatal("no thumbnails in HTML")
	}
	for i, m := range thumbs {
		data, _ := base64.StdEncoding.DecodeString(m[1])
		big := enlarge(t, data, 3)
		lines, err := r.Read(context.Background(), big, ocr.Page)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			text, _ := l.Text()
			if fs := eng.Scan(text); len(fs) > 0 {
				t.Errorf("thumbnail %d: secret readable: %s %s", i, fs[0].Label, fs[0].Masked)
			}
		}
	}
}

// enlarge scales a JPEG k times with a sharp filter and returns it gray.
func enlarge(t *testing.T, jpg []byte, k int) *image.Gray {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatal(err)
	}
	w, h := img.Bounds().Dx()*k, img.Bounds().Dy()*k
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "jpeg_pipe", "-i", "-",
		"-vf", "scale="+strconv.Itoa(w)+":"+strconv.Itoa(h)+":flags=lanczos,format=gray", "-f", "rawvideo", "-")
	cmd.Stdin = bytes.NewReader(jpg)
	pix, err := cmd.Output()
	if err != nil || len(pix) != w*h {
		t.Fatalf("ffmpeg scale: %v", err)
	}
	return &image.Gray{Pix: pix, Stride: w, Rect: image.Rect(0, 0, w, h)}
}

func TestExitCodes(t *testing.T) {
	if o := runArgs("does-not-exist.mp4"); o.code != exitError || !strings.Contains(o.stderr, "does-not-exist.mp4") {
		t.Errorf("missing file: %+v", o)
	}
	if o := runArgs(); o.code != exitError {
		t.Errorf("no video: exit %d", o.code)
	}
	if o := runArgs("x.mp4", "--only", "colors"); o.code != exitError || !strings.Contains(o.stderr, "colors") {
		t.Errorf("bad check: %+v", o)
	}
	if o := runArgs("--help"); o.code != exitOK || !strings.Contains(o.stdout, "Usage:") {
		t.Errorf("--help: %+v", o)
	}
	if o := runArgs(video(t, "clean.mp4"), "--env", "missing.env"); o.code != exitError {
		t.Errorf("missing .env: exit %d", o.code)
	}
}
