package report

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"slices"

	"github.com/smanookian/leakinator/internal/media"
	"github.com/smanookian/leakinator/internal/scan"
)

const thumbWidth = 640

// HTML writes a self-contained report page. Thumbnails are cut from the
// video; every secret found on a thumbnail is pixelated beyond reading.
func (r *Report) HTML(ctx context.Context, w io.Writer, video string) error {
	type row struct {
		Bad   bool
		When  string
		Text  string
		Thumb template.URL
	}
	var secrets, small []row
	for i, s := range r.Secrets {
		th, err := r.thumb(ctx, video, r.secretThumbs[i])
		if err != nil {
			return err
		}
		secrets = append(secrets, row{Bad: true, When: span(s.Start, s.End), Text: s.what(), Thumb: th})
	}
	for i, t := range r.SmallText {
		th, err := r.thumb(ctx, video, r.smallThumbs[i])
		if err != nil {
			return err
		}
		small = append(small, row{When: span(t.Start, t.End),
			Text: fmt.Sprintf("Text too small (about %.0f px tall, needs %.0f)", t.HeightPx, t.MinPx), Thumb: th})
	}
	return page.Execute(w, map[string]any{
		"R": r, "Secrets": secrets, "Small": small, "Clock": Clock(r.Duration),
		"HasSecrets": slices.Contains(r.Checks, "secrets"), "HasText": slices.Contains(r.Checks, "text"), "HasAudio": slices.Contains(r.Checks, "audio"),
	})
}

// thumb returns a JPEG data URL of the frame at th.T with secrets hidden.
func (r *Report) thumb(ctx context.Context, video string, th scan.Thumb) (template.URL, error) {
	img, err := media.Still(ctx, video, th.T, thumbWidth)
	if err != nil {
		return "", err
	}
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Src)
	sx := float64(dst.Bounds().Dx()) / float64(r.frameW)
	sy := float64(dst.Bounds().Dy()) / float64(r.frameH)
	for _, b := range th.Blur {
		box := image.Rect(int(float64(b.Min.X)*sx), int(float64(b.Min.Y)*sy),
			int(float64(b.Max.X)*sx+0.999), int(float64(b.Max.Y)*sy+0.999))
		hide(dst, box)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return "", err
	}
	return template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())), nil
}

// hide pixelates box (grown by a margin) with blocks larger than the text,
// then draws a red frame around it.
func hide(img *image.RGBA, box image.Rectangle) {
	pad := max(4, box.Dy()/2)
	box = box.Inset(-pad).Intersect(img.Bounds())
	if box.Empty() {
		return
	}
	cell := max(12, box.Dy())
	for y := box.Min.Y; y < box.Max.Y; y += cell {
		for x := box.Min.X; x < box.Max.X; x += cell {
			c := image.Rect(x, y, x+cell, y+cell).Intersect(box)
			var r, g, b, n uint32
			for py := c.Min.Y; py < c.Max.Y; py++ {
				for px := c.Min.X; px < c.Max.X; px++ {
					o := img.PixOffset(px, py)
					r += uint32(img.Pix[o])
					g += uint32(img.Pix[o+1])
					b += uint32(img.Pix[o+2])
					n++
				}
			}
			avg := color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255}
			draw.Draw(img, c, &image.Uniform{avg}, image.Point{}, draw.Src)
		}
	}
	red := &image.Uniform{color.RGBA{230, 40, 40, 255}}
	for _, edge := range []image.Rectangle{
		{box.Min, image.Pt(box.Max.X, box.Min.Y+2)}, {image.Pt(box.Min.X, box.Max.Y-2), box.Max},
		{box.Min, image.Pt(box.Min.X+2, box.Max.Y)}, {image.Pt(box.Max.X-2, box.Min.Y), box.Max},
	} {
		draw.Draw(img, edge, red, image.Point{}, draw.Src)
	}
}

var page = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Leakinator: {{.R.File}}</title>
<style>
body{font:15px/1.5 system-ui,sans-serif;max-width:900px;margin:2em auto;padding:0 1em;color:#222}
h1{font-size:1.4em}h2{font-size:1.15em;margin-top:2em;border-bottom:1px solid #ddd}
.item{display:flex;gap:1em;align-items:flex-start;margin:1em 0}
.item img{width:320px;border:1px solid #ccc;border-radius:4px}
.when{font-family:ui-monospace,monospace;font-weight:600}
.bad{color:#c62828}.warn{color:#b26a00}.ok{color:#2e7d32}
.summary{font-size:1.1em;font-weight:600;padding:.7em 1em;border-radius:6px;background:#f5f5f5}
</style></head><body>
<h1>Leakinator report</h1>
<p>{{.R.File}} · {{.Clock}} · {{.R.Width}}x{{.R.Height}}</p>
<p class="summary">
{{- if .R.Summary.Secrets}}<span class="bad">{{.R.Summary.Secrets}} secret(s) found. Do not upload.</span> {{end -}}
{{- if .R.Summary.Warnings}}<span class="warn">{{.R.Summary.Warnings}} warning(s).</span>{{end -}}
{{- if not (or .R.Summary.Secrets .R.Summary.Warnings)}}<span class="ok">All good.</span>{{end -}}
</p>
{{if .HasSecrets}}<h2>Secrets</h2>
{{range .Secrets}}<div class="item"><img src="{{.Thumb}}" alt="frame at {{.When}}"><div><span class="when bad">{{.When}}</span><br>{{.Text}}</div></div>
{{else}}<p class="ok">No secrets found.</p>{{end}}
<p><small>Found secrets are pixelated in the pictures. Only the first 4 characters are shown.</small></p>{{end}}
{{if .HasText}}<h2>Text size</h2>
{{range .Small}}<div class="item"><img src="{{.Thumb}}" alt="frame at {{.When}}"><div><span class="when warn">{{.When}}</span><br>{{.Text}}</div></div>
{{else}}<p class="ok">Text is big enough for a phone.</p>{{end}}{{end}}
{{if .HasAudio}}<h2>Audio</h2>
{{if .R.NoAudio}}<p>No audio track.</p>{{else if .R.Audio}}{{with .R.Audio}}
<p>Loudness <b>{{printf "%.1f" .LUFS}} LUFS</b> (YouTube: {{printf "%.0f" .Target}}), true peak <b>{{printf "%+.1f" .TruePeak}} dBTP</b>, range {{printf "%.1f" .LRA}} LU.</p>
{{range .Problems}}<p class="warn">! {{.}}</p>{{else}}<p class="ok">Audio is fine.</p>{{end}}{{end}}{{end}}{{end}}
<p><small>Made by leakinator in {{printf "%.0f" .R.Stats.Seconds}} s. Checked {{.R.Stats.FramesChecked}} frames, read text {{.R.Stats.OCRReads}} times.</small></p>
</body></html>
`))
