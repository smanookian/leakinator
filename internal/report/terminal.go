package report

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/smanookian/leakinator/internal/audio"
)

// Terminal writes the short list. color turns on red/yellow/green.
func (r *Report) Terminal(w io.Writer, color bool) {
	c := func(code, s string) string {
		if !color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	red := func(s string) string { return c("31", s) }
	yellow := func(s string) string { return c("33", s) }
	green := func(s string) string { return c("32", s) }
	bold := func(s string) string { return c("1", s) }
	bad, warn, ok := red("✗"), yellow("!"), green("✓")

	fmt.Fprintf(w, "%s  %s  (%s, %dx%d)\n", bold("leakinator"), r.File, Clock(r.Duration), r.Width, r.Height)
	has := func(check string) bool { return slices.Contains(r.Checks, check) }

	if has("secrets") {
		fmt.Fprintf(w, "\n%s\n", bold("Secrets"))
		if len(r.Secrets) == 0 {
			fmt.Fprintf(w, "  %s No secrets found\n", ok)
		}
		for _, s := range r.Secrets {
			fmt.Fprintf(w, "  %s %-13s %s\n", bad, span(s.Start, s.End), s.what())
		}
	}
	if has("text") {
		fmt.Fprintf(w, "\n%s\n", bold("Text size"))
		if len(r.SmallText) == 0 {
			fmt.Fprintf(w, "  %s Text is big enough for a phone\n", ok)
		}
		for _, t := range r.SmallText {
			fmt.Fprintf(w, "  %s %-13s text too small (about %.0f px tall, needs %.0f)\n", warn, span(t.Start, t.End), t.HeightPx, t.MinPx)
		}
	}
	if has("audio") {
		fmt.Fprintf(w, "\n%s\n", bold("Audio"))
		switch a := r.Audio; {
		case r.NoAudio:
			fmt.Fprintf(w, "  - No audio track\n")
		case a != nil:
			if a.LUFS >= audio.TooQuiet && a.LUFS <= audio.TooLoud && a.TruePeak <= audio.MaxTruePeak {
				fmt.Fprintf(w, "  %s Loudness %.1f LUFS, true peak %+.1f dBTP (YouTube: %.0f LUFS, peak below %.0f)\n",
					ok, a.LUFS, a.TruePeak, a.Target, audio.MaxTruePeak)
			}
			const most = 10
			for i, p := range a.Problems {
				if i == most {
					fmt.Fprintf(w, "  %s …and %d more\n", warn, len(a.Problems)-most)
					break
				}
				fmt.Fprintf(w, "  %s %s\n", warn, p)
			}
		}
	}

	fmt.Fprintln(w)
	var parts []string
	if n := r.Summary.Secrets; n > 0 {
		parts = append(parts, red(bold(fmt.Sprintf("%d %s found. Do not upload.", n, plural(n, "secret", "secrets")))))
	}
	if n := r.Summary.Warnings; n > 0 {
		parts = append(parts, yellow(fmt.Sprintf("%d %s.", n, plural(n, "warning", "warnings"))))
	}
	if len(parts) == 0 {
		parts = append(parts, green("All good."))
	}
	fmt.Fprintf(w, "%s  (checked in %.0f s)\n", strings.Join(parts, " "), r.Stats.Seconds)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
