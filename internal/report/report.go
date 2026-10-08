// Package report turns check results into a terminal list, JSON or HTML.
//
// Nothing here ever sees a secret value: findings only carry the first
// 4 characters (secretengine.Finding.Masked).
package report

import (
	"fmt"
	"math"

	"github.com/smanookian/leakinator/internal/audio"
	"github.com/smanookian/leakinator/internal/scan"
)

// Report is the JSON output. Field names are stable; new fields may be added.
type Report struct {
	Version   int         `json:"version"`
	File      string      `json:"file"`
	Duration  float64     `json:"duration"`
	Width     int         `json:"width"`
	Height    int         `json:"height"`
	Checks    []string    `json:"checks"`
	Secrets   []Secret    `json:"secrets"`
	SmallText []SmallText `json:"small_text"`
	Audio     *Audio      `json:"audio"` // null when not checked or no audio track
	NoAudio   bool        `json:"no_audio,omitempty"`
	Summary   Summary     `json:"summary"`
	Stats     Stats       `json:"stats"`

	frameW, frameH int
	secretThumbs   []scan.Thumb
	smallThumbs    []scan.Thumb
}

// Secret is one secret on screen.
type Secret struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Kind    string  `json:"kind"` // exact, partial, pattern
	Rule    string  `json:"rule"` // env or a pattern ID
	Label   string  `json:"label"`
	Source  string  `json:"source,omitempty"`
	Masked  string  `json:"masked"`
	Matched int     `json:"matched,omitempty"`
	Of      int     `json:"of,omitempty"`
	Box     [4]int  `json:"box"` // x, y, w, h in video pixels where first seen
}

// SmallText is a time range with text too small for a phone.
type SmallText struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	HeightPx float64 `json:"height_px"` // letter height at 1080p
	MinPx    float64 `json:"min_px"`
}

// Audio is the loudness check.
type Audio struct {
	audio.Result
	Target   float64  `json:"target_lufs"`
	Problems []string `json:"problems"`
}

// Summary counts issues. Any secret means: don't upload.
type Summary struct {
	Secrets  int `json:"secrets"`
	Warnings int `json:"warnings"`
}

// Stats says how much work was done.
type Stats struct {
	FramesChecked int     `json:"frames_checked"`
	OCRReads      int     `json:"ocr_reads"`
	Seconds       float64 `json:"seconds"`
}

// Input is everything Build needs.
type Input struct {
	File           string
	Duration       float64
	Width, Height  int
	Checks         []string
	Scan           *scan.Result // nil if no frame checks ran
	MinText        float64
	Audio          *audio.Result // nil if not checked or no audio
	AudioChecked   bool
	ElapsedSeconds float64
}

// Build makes a Report.
func Build(in Input) *Report {
	r := &Report{
		Version: 1, File: in.File, Duration: round(in.Duration), Width: in.Width, Height: in.Height,
		Checks: in.Checks, Secrets: []Secret{}, SmallText: []SmallText{},
		Stats: Stats{Seconds: round(in.ElapsedSeconds)},
	}
	if s := in.Scan; s != nil {
		r.frameW, r.frameH = s.FrameW, s.FrameH
		r.Stats.FramesChecked, r.Stats.OCRReads = s.Samples, s.Reads
		sx := float64(in.Width) / float64(s.FrameW)
		sy := float64(in.Height) / float64(s.FrameH)
		for _, h := range s.Secrets {
			b := h.Box
			r.Secrets = append(r.Secrets, Secret{
				Start: round(h.Start), End: round(h.End), Kind: string(h.Kind), Rule: h.Rule, Label: h.Label,
				Source: h.Source, Masked: h.Masked, Matched: h.Matched, Of: h.Of,
				Box: [4]int{int(float64(b.Min.X) * sx), int(float64(b.Min.Y) * sy), int(float64(b.Dx()) * sx), int(float64(b.Dy()) * sy)},
			})
			r.secretThumbs = append(r.secretThumbs, h.Thumb)
		}
		for _, t := range s.SmallText {
			r.SmallText = append(r.SmallText, SmallText{Start: round(t.Start), End: round(t.End), HeightPx: math.Round(t.Height*10) / 10, MinPx: in.MinText})
			r.smallThumbs = append(r.smallThumbs, t.Thumb)
		}
	}
	if in.AudioChecked && in.Audio == nil {
		r.NoAudio = true
	}
	if a := in.Audio; a != nil {
		r.Audio = &Audio{Result: *a, Target: audio.Target, Problems: audioProblems(a)}
		r.Summary.Warnings += len(r.Audio.Problems)
	}
	r.Summary.Secrets = len(r.Secrets)
	r.Summary.Warnings += len(r.SmallText)
	return r
}

func audioProblems(a *audio.Result) []string {
	var p []string
	switch {
	case a.LUFS > audio.TooLoud:
		p = append(p, fmt.Sprintf("Loudness %.1f LUFS is louder than YouTube's %.0f. YouTube will turn it down.", a.LUFS, audio.Target))
	case a.LUFS < audio.TooQuiet:
		p = append(p, fmt.Sprintf("Loudness %.1f LUFS is quieter than YouTube's %.0f. It will sound quiet.", a.LUFS, audio.Target))
	}
	if a.TruePeak > audio.MaxTruePeak {
		p = append(p, fmt.Sprintf("True peak %+.1f dBTP is above %.0f. It may distort.", a.TruePeak, audio.MaxTruePeak))
	}
	for _, c := range a.Clipping {
		p = append(p, "Clipping at "+span(c.Start, c.End))
	}
	for _, s := range a.Silences {
		p = append(p, fmt.Sprintf("Silence at %s (%.0f s)", span(s.Start, s.End), s.End-s.Start))
	}
	return p
}

func round(v float64) float64 { return math.Round(v*100) / 100 }

// Clock formats seconds as mm:ss, or h:mm:ss for long videos.
func Clock(sec float64) string {
	s := int(math.Max(0, sec))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// span formats a time range in whole seconds (00:05-00:07).
func span(start, end float64) string {
	e := math.Round(end)
	if e <= math.Floor(start) {
		return Clock(start)
	}
	return Clock(start) + "-" + Clock(e)
}

func (s Secret) what() string {
	text := s.Label
	if s.Rule == "env" {
		text += " from " + s.Source
		if s.Kind == "partial" {
			text += fmt.Sprintf(", partly visible (%d of %d characters)", s.Matched, s.Of)
		}
	}
	return text + " (" + s.Masked + ")"
}
