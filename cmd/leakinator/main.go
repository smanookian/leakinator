// Command leakinator checks a coding video before you upload it:
// secrets on screen, text too small for a phone, and loudness.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/smanookian/leakinator/internal/audio"
	"github.com/smanookian/leakinator/internal/media"
	"github.com/smanookian/leakinator/internal/report"
	"github.com/smanookian/leakinator/internal/scan"
	"github.com/smanookian/leakinator/secretengine"
)

var version = "dev"

// Exit codes.
const (
	exitOK     = 0
	exitSecret = 1
	exitError  = 2
)

const usage = `leakinator checks a coding video before you upload it.

Usage:
  leakinator VIDEO [options]

Examples:
  leakinator video.mp4
  leakinator video.mp4 --env .env --env ~/project/.env
  leakinator video.mp4 --only secrets
  leakinator video.mp4 --html report.html

Options:
  --env FILE       Look for the values in this .env file (use it more than once
                   for more files). Values are never shown or saved.
  --only LIST      Run only these checks: secrets, text, audio (comma list).
  --json           Print the result as JSON instead of the list.
  --html FILE      Also write an HTML report with pictures (secrets pixelated).
  --min-text PX    Smallest OK letter height at 1080p (default 14).
  --fps N          Frames looked at per second (default 2).
  --no-color       No colors.
  --version        Show the version.

Exit code: 0 no secrets, 1 secrets found, 2 error.
Needs ffmpeg and tesseract. Everything runs on your computer.
`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

type options struct {
	video   string
	envs    multi
	only    multi
	json    bool
	html    string
	minText float64
	fps     float64
	noColor bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	o, code, ok := parse(args, stdout, stderr)
	if !ok {
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := check(ctx, o, stdout, stderr, &code); err != nil {
		fmt.Fprintln(stderr, "leakinator:", err)
		return exitError
	}
	return code
}

func parse(args []string, stdout, stderr io.Writer) (o options, code int, ok bool) {
	fs := flag.NewFlagSet("leakinator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&o.envs, "env", "")
	fs.Var(&o.only, "only", "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.StringVar(&o.html, "html", "", "")
	fs.Float64Var(&o.minText, "min-text", 14, "")
	fs.Float64Var(&o.fps, "fps", 2, "")
	fs.BoolVar(&o.noColor, "no-color", false, "")
	help := fs.Bool("help", false, "")
	fs.BoolVar(help, "h", false, "")
	ver := fs.Bool("version", false, "")

	// Flags may come before or after the video.
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			fmt.Fprintf(stderr, "leakinator: %v\nRun 'leakinator --help' for help.\n", err)
			return o, exitError, false
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	switch {
	case *help:
		fmt.Fprint(stdout, usage)
		return o, exitOK, false
	case *ver:
		fmt.Fprintln(stdout, "leakinator", version)
		return o, exitOK, false
	case len(pos) != 1:
		fmt.Fprint(stderr, usage)
		return o, exitError, false
	}
	o.video = pos[0]
	for _, c := range o.checks() {
		if !slices.Contains([]string{"secrets", "text", "audio"}, c) {
			fmt.Fprintf(stderr, "leakinator: unknown check %q (use secrets, text, audio)\n", c)
			return o, exitError, false
		}
	}
	if o.minText <= 0 || o.fps <= 0 {
		fmt.Fprintln(stderr, "leakinator: --min-text and --fps must be above 0")
		return o, exitError, false
	}
	return o, exitOK, true
}

func (o options) checks() []string {
	if len(o.only) == 0 {
		return []string{"secrets", "text", "audio"}
	}
	var out []string
	for _, v := range o.only {
		for _, c := range strings.Split(v, ",") {
			if c = strings.TrimSpace(c); c != "" && !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

func check(ctx context.Context, o options, stdout, stderr io.Writer, code *int) error {
	start := time.Now()
	checks := o.checks()
	want := func(c string) bool { return slices.Contains(checks, c) }

	var secrets []secretengine.Secret
	for _, path := range o.envs {
		s, err := secretengine.LoadEnvFile(path)
		if err != nil {
			return err
		}
		secrets = append(secrets, s...)
	}
	if _, err := os.Stat(o.video); err != nil {
		return err
	}
	info, err := media.Probe(ctx, o.video)
	if err != nil {
		return err
	}

	in := report.Input{File: o.video, Duration: info.Duration, Width: info.Width, Height: info.Height,
		Checks: checks, MinText: o.minText}

	progress := func(float64) {}
	if isTerminal(stderr) {
		var mu sync.Mutex
		last := -1
		progress = func(p float64) {
			mu.Lock()
			defer mu.Unlock()
			if pct := int(p * 100); pct != last {
				last = pct
				fmt.Fprintf(stderr, "\rChecking… %d%% ", pct)
			}
		}
		defer fmt.Fprint(stderr, "\r              \r")
	}

	var wg sync.WaitGroup
	var scanErr, audioErr error
	if (want("secrets") || want("text")) && info.HasVideo {
		opt := scan.Options{FPS: o.fps, TextSize: want("text"), MinText: o.minText, Progress: progress}
		if want("secrets") {
			opt.Secrets = secretengine.New(secretengine.Options{Secrets: secrets})
		}
		wg.Go(func() { in.Scan, scanErr = scan.Video(ctx, o.video, info, opt) })
	}
	if want("audio") {
		in.AudioChecked = true
		if info.HasAudio {
			wg.Go(func() { in.Audio, audioErr = audio.Check(ctx, o.video, info.Duration) })
		}
	}
	wg.Wait()
	if err := errors.Join(scanErr, audioErr); err != nil {
		return err
	}
	in.ElapsedSeconds = time.Since(start).Seconds()
	rep := report.Build(in)

	if o.html != "" {
		f, err := os.Create(o.html)
		if err != nil {
			return err
		}
		if err := rep.HTML(ctx, f, o.video); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if o.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		color := !o.noColor && os.Getenv("NO_COLOR") == "" && isTerminal(stdout)
		rep.Terminal(stdout, color)
		if o.html != "" {
			fmt.Fprintf(stdout, "HTML report: %s\n", o.html)
		}
	}
	if rep.Summary.Secrets > 0 {
		*code = exitSecret
	}
	return nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
