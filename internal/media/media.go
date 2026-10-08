// Package media runs ffmpeg and ffprobe: video info, gray frames, thumbnails.
package media

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

// Tool finds a program in PATH. On Windows it also looks in the usual
// install folders. The env var LEAKINATOR_<NAME> (e.g. LEAKINATOR_TESSERACT)
// can point to the program.
func Tool(name string) (string, error) {
	if p := os.Getenv("LEAKINATOR_" + upper(name)); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		for _, dir := range []string{`C:\Program Files\Tesseract-OCR`, `C:\Program Files\ffmpeg\bin`, `C:\ffmpeg\bin`} {
			p := filepath.Join(dir, name+".exe")
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s not found. Install it and make sure it is in your PATH", name)
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// Info describes a video file.
type Info struct {
	Width, Height int
	Duration      float64 // seconds
	HasVideo      bool
	HasAudio      bool
}

// Probe reads basic facts about a video with ffprobe.
func Probe(ctx context.Context, path string) (Info, error) {
	ffprobe, err := Tool("ffprobe")
	if err != nil {
		return Info{}, err
	}
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-print_format", "json",
		"-show_entries", "format=duration:stream=codec_type,width,height,duration", path).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return Info{}, fmt.Errorf("can't read %s: %s", path, bytes.TrimSpace(ee.Stderr))
		}
		return Info{}, fmt.Errorf("can't read %s: %w", path, err)
	}
	var p struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Duration  string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w", err)
	}
	var info Info
	info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if !info.HasVideo {
				info.HasVideo, info.Width, info.Height = true, s.Width, s.Height
				if info.Duration == 0 {
					info.Duration, _ = strconv.ParseFloat(s.Duration, 64)
				}
			}
		case "audio":
			info.HasAudio = true
		}
	}
	if !info.HasVideo && !info.HasAudio {
		return Info{}, fmt.Errorf("%s has no video or audio", path)
	}
	return info, nil
}

// FrameSize is the size frames are analyzed at: the video size, but
// never taller than maxHeight. Width and height are even.
func FrameSize(info Info, maxHeight int) (w, h int) {
	w, h = info.Width, info.Height
	if h > maxHeight {
		w = int(float64(w)*float64(maxHeight)/float64(h)+0.5) &^ 1
		h = maxHeight
	}
	return w &^ 1, h &^ 1
}

// Frames decodes fps gray frames per second at size w×h and calls fn for
// each. The frame buffer is reused: fn must copy what it keeps.
func Frames(ctx context.Context, path string, fps float64, w, h int, fn func(index int, t float64, img *image.Gray) error) error {
	ffmpeg, err := Tool("ffmpeg")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", path, "-map", "0:v:0", "-an", "-sn", "-dn",
		"-vf", fmt.Sprintf("fps=%g,scale=%d:%d:flags=area,format=gray", fps, w, h),
		"-f", "rawvideo", "-pix_fmt", "gray", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	r := bufio.NewReaderSize(out, 1<<20)
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := 0; ; i++ {
		if _, err := io.ReadFull(r, img.Pix); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}
		if err := fn(i, float64(i)/fps, img); err != nil {
			return err
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %s", bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
}

// Still returns one color frame at time t, width w (height keeps the ratio).
func Still(ctx context.Context, path string, t float64, w int) (image.Image, error) {
	ffmpeg, err := Tool("ffmpeg")
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-ss", strconv.FormatFloat(t, 'f', 3, 64), "-i", path, "-map", "0:v:0", "-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-2", w), "-f", "image2pipe", "-c:v", "png", "-").Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg still at %.1fs: %w", t, err)
	}
	return png.Decode(bytes.NewReader(out))
}
