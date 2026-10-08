// Package scan looks at the frames of a video: secrets and text size.
//
// It decodes a few frames per second, reads text (OCR) only where the
// screen changed, and keeps a model of the text on screen over time.
package scan

import (
	"context"
	"image"
	"runtime"
	"sort"
	"sync"

	"github.com/smanookian/leakinator/internal/media"
	"github.com/smanookian/leakinator/internal/ocr"
	"github.com/smanookian/leakinator/secretengine"
)

// Options for Video. Zero values use the defaults.
type Options struct {
	FPS       float64              // frames looked at per second (default 2)
	Workers   int                  // OCR programs run at once (default: CPU cores)
	Secrets   *secretengine.Engine // nil: don't look for secrets
	TextSize  bool                 // check text size
	MinText   float64              // smallest OK letter height in px at 1080p (default 14)
	MaxHeight int                  // frames taller than this are scaled down (default 1080)
	Progress  func(done float64)   // called with 0..1
}

// Thumb says which frame shows an issue and which boxes must be blurred.
type Thumb struct {
	T    float64
	Blur []image.Rectangle // every secret on screen at T (frame pixels)
}

// SecretHit is a secret on screen from Start to End (seconds).
type SecretHit struct {
	secretengine.Finding
	Start, End float64
	Box        image.Rectangle // where it was first seen (frame pixels)
	Thumb      Thumb
}

// SmallText is a time range where most text is too small.
type SmallText struct {
	Start, End float64
	Height     float64 // typical letter height in px at 1080p
	Thumb      Thumb
}

// Result of Video.
type Result struct {
	FrameW, FrameH int // size frames were analyzed at; boxes use it
	Samples        int // frames looked at
	Reads          int // OCR calls
	FullReads      int // OCR calls on whole frames
	Secrets        []SecretHit
	SmallText      []SmallText
}

type job struct {
	seq, sample int
	t           float64
	full        bool
	keep        band // lines with their middle in here are replaced
	img         *image.Gray
}

type hit struct {
	f   secretengine.Finding
	box image.Rectangle
}

type textLine struct {
	box     image.Rectangle
	hits    []hit
	heights []float64
}

type result struct {
	seq, sample int
	t           float64
	full        bool
	keep        band
	lines       []textLine
}

// Video scans the frames of a video.
func Video(ctx context.Context, path string, info media.Info, o Options) (*Result, error) {
	if o.FPS <= 0 {
		o.FPS = 2
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	if o.MinText <= 0 {
		o.MinText = 14
	}
	if o.MaxHeight <= 0 {
		o.MaxHeight = 1080
	}
	reader, err := ocr.New()
	if err != nil {
		return nil, err
	}
	fw, fh := media.FrameSize(info, o.MaxHeight)
	res := &Result{FrameW: fw, FrameH: fh}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	jobs := make(chan job, o.Workers*2)
	var (
		mu      sync.Mutex
		results []result
		wg      sync.WaitGroup
	)
	for range o.Workers {
		wg.Go(func() {
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				mode := ocr.Block
				if j.full {
					mode = ocr.Page
				}
				lines, err := reader.Read(ctx, j.img, mode)
				if err != nil {
					cancel(err)
					continue
				}
				r := result{seq: j.seq, sample: j.sample, t: j.t, full: j.full, keep: j.keep, lines: analyze(lines, o)}
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		})
	}

	// Strips are read with a margin so lines at their edges are whole.
	margin := max(24, fh*48/1080)
	var det detector
	seq := 0
	send := func(j job) error {
		select {
		case jobs <- j:
			seq++
			res.Reads++
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	err = media.Frames(ctx, path, o.FPS, fw, fh, func(i int, t float64, img *image.Gray) error {
		res.Samples = i + 1
		if o.Progress != nil && info.Duration > 0 {
			o.Progress(min(1, t/info.Duration))
		}
		full, bands := det.next(img)
		if full {
			res.FullReads++
			return send(job{seq: seq, sample: i, t: t, full: true, keep: band{0, fh}, img: rows(img, 0, fh)})
		}
		for _, b := range bands {
			keep := band{max(0, b.y0-block/2), min(fh, b.y1+block/2)}
			if err := send(job{seq: seq, sample: i, t: t, keep: keep,
				img: rows(img, max(0, b.y0-margin), min(fh, b.y1+margin))}); err != nil {
				return err
			}
		}
		return nil
	})
	close(jobs)
	wg.Wait()
	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	if err != nil {
		return nil, err
	}

	end := info.Duration
	if end <= 0 {
		end = float64(res.Samples) / o.FPS
	}
	sort.Slice(results, func(i, j int) bool { return results[i].seq < results[j].seq })
	replay(res, results, end, o)
	return res, nil
}

// rows copies rows [y0, y1) of img into a new compact image.
func rows(img *image.Gray, y0, y1 int) *image.Gray {
	out := image.NewGray(image.Rect(0, y0, img.Rect.Dx(), y1))
	copy(out.Pix, img.Pix[y0*img.Stride:y1*img.Stride])
	return out
}

// analyze finds secrets and letter heights in OCR lines.
func analyze(lines []ocr.Line, o Options) []textLine {
	out := make([]textLine, 0, len(lines))
	for _, l := range lines {
		tl := textLine{box: l.Box}
		if o.Secrets != nil {
			text, offs := l.Text()
			for _, f := range o.Secrets.Scan(text) {
				var box image.Rectangle
				for k, w := range l.Words {
					if offs[k] < f.End && f.Start < offs[k]+len(w.Text) {
						box = box.Union(w.Box)
					}
				}
				tl.hits = append(tl.hits, hit{f: f, box: box})
			}
		}
		if o.TextSize {
			tl.heights = letterHeights(l.Words)
		}
		out = append(out, tl)
	}
	return out
}

func mid(r image.Rectangle) int { return (r.Min.Y + r.Max.Y) / 2 }

// replay applies OCR results in time order to a model of the text on
// screen and turns the model's states into time ranges.
func replay(res *Result, results []result, end float64, o Options) {
	var model []textLine
	open := map[string]*SecretHit{}
	var hits []SecretHit
	var small *SmallText
	var smallHeights []float64
	var smalls []SmallText
	closeSmall := func(t float64) {
		if small != nil {
			small.End = t
			small.Height = median(smallHeights)
			smalls = append(smalls, *small)
			small, smallHeights = nil, nil
		}
	}

	eval := func(t float64) {
		// Secrets on screen now.
		present := map[string]hit{}
		var blur []image.Rectangle
		for _, l := range model {
			for _, h := range l.hits {
				k := h.f.Rule + "\x00" + h.f.Label + "\x00" + h.f.Masked
				if old, ok := present[k]; !ok || (old.f.Kind != secretengine.Exact && h.f.Kind == secretengine.Exact) {
					present[k] = h
				}
				blur = append(blur, h.box)
			}
		}
		for k, h := range present {
			if s, ok := open[k]; ok {
				if s.Kind == secretengine.Partial && h.f.Kind == secretengine.Exact {
					s.Kind, s.Matched = h.f.Kind, h.f.Matched
				}
				continue
			}
			open[k] = &SecretHit{Finding: h.f, Start: t, Box: h.box, Thumb: Thumb{T: t, Blur: blur}}
		}
		for k, s := range open {
			if _, ok := present[k]; !ok {
				s.End = t
				hits = append(hits, *s)
				delete(open, k)
			}
		}

		// Text size now.
		if !o.TextSize {
			return
		}
		var hs []float64
		for _, l := range model {
			hs = append(hs, l.heights...)
		}
		if len(hs) < 5 {
			closeSmall(t)
			return
		}
		h := median(hs) * 1080 / float64(res.FrameH)
		if h >= o.MinText {
			closeSmall(t)
			return
		}
		if small == nil {
			small = &SmallText{Start: t, Thumb: Thumb{T: t, Blur: blur}}
		}
		smallHeights = append(smallHeights, h)
	}

	for i := 0; i < len(results); {
		sample := results[i].sample
		t := results[i].t
		for ; i < len(results) && results[i].sample == sample; i++ {
			r := results[i]
			if r.full {
				model = r.lines
				continue
			}
			kept := model[:0:0]
			for _, l := range model {
				if y := mid(l.box); y < r.keep.y0 || y >= r.keep.y1 {
					kept = append(kept, l)
				}
			}
			for _, l := range r.lines {
				if y := mid(l.box); y >= r.keep.y0 && y < r.keep.y1 {
					kept = append(kept, l)
				}
			}
			model = kept
		}
		eval(t)
	}
	for _, s := range open {
		s.End = end
		hits = append(hits, *s)
	}
	closeSmall(end)

	res.Secrets = mergeHits(hits)
	res.SmallText = mergeSmall(smalls)
}

// gap: ranges of the same thing closer than this are joined
// (OCR can miss a key in one frame). minSmall: shorter small-text ranges
// are dropped.
const (
	gap      = 1.0
	minSmall = 1.0
)

func mergeHits(hits []SecretHit) []SecretHit {
	sort.Slice(hits, func(i, j int) bool { return hits[i].Start < hits[j].Start })
	var out []SecretHit
	last := map[string]int{}
	for _, h := range hits {
		k := h.Rule + "\x00" + h.Label + "\x00" + h.Masked
		if i, ok := last[k]; ok && h.Start-out[i].End <= gap {
			out[i].End = max(out[i].End, h.End)
			if h.Kind == secretengine.Exact {
				out[i].Kind, out[i].Matched = h.Kind, h.Matched
			}
			continue
		}
		last[k] = len(out)
		out = append(out, h)
	}
	return out
}

func mergeSmall(r []SmallText) []SmallText {
	var out []SmallText
	for _, s := range r {
		if n := len(out); n > 0 && s.Start-out[n-1].End <= gap {
			out[n-1].End = s.End
			out[n-1].Height = min(out[n-1].Height, s.Height)
			continue
		}
		out = append(out, s)
	}
	kept := out[:0]
	for _, s := range out {
		if s.End-s.Start >= minSmall {
			kept = append(kept, s)
		}
	}
	return kept
}
