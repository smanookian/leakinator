// Package audio measures loudness (ITU BS.1770 via ffmpeg's ebur128),
// true peak, clipping and long silences.
package audio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/smanookian/leakinator/internal/media"
)

// Limits for the loudness check.
const (
	Target       = -14.0 // YouTube's loudness target, LUFS
	TooLoud      = -13.0 // louder than this: YouTube turns it down
	TooQuiet     = -16.0 // quieter than this: sounds quieter than other videos
	MaxTruePeak  = -1.0  // dBTP
	ClipLevel    = 0.999 // a sample at or above this is at full scale
	ClipRun      = 3     // this many full-scale samples in a row is clipping
	SilenceDB    = -50.0 // quieter than this is silence, dB
	SilenceSecs  = 4.0   // silence at least this long is reported
	clipJoinSecs = 0.5   // clipping closer than this is one range
)

// Range is a time range in seconds.
type Range struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Result of Check.
type Result struct {
	LUFS     float64 `json:"lufs"`      // integrated loudness
	TruePeak float64 `json:"true_peak"` // dBTP
	LRA      float64 `json:"lra"`       // loudness range, LU
	Clipping []Range `json:"clipping"`
	Silences []Range `json:"silences"`
}

var (
	reI       = regexp.MustCompile(`(?m)^\s+I:\s+(-?[\d.]+|-inf) LUFS`)
	reLRA     = regexp.MustCompile(`(?m)^\s+LRA:\s+(-?[\d.]+) LU`)
	rePeak    = regexp.MustCompile(`(?m)^\s+Peak:\s+(-?[\d.]+|-inf) dBFS`)
	reSilence = regexp.MustCompile(`silence_(start|end): (-?[\d.]+)`)
)

// Check measures the first audio track of a video.
func Check(ctx context.Context, path string, duration float64) (*Result, error) {
	ffmpeg, err := media.Tool("ffmpeg")
	if err != nil {
		return nil, err
	}
	type clipOut struct {
		r   []Range
		err error
	}
	clipCh := make(chan clipOut, 1)
	go func() {
		r, err := clipping(ctx, ffmpeg, path)
		clipCh <- clipOut{r, err}
	}()

	out, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-nostats", "-hide_banner", "-i", path,
		"-map", "0:a:0", "-af", fmt.Sprintf("ebur128=peak=true:framelog=quiet,silencedetect=noise=%gdB:d=%g", SilenceDB, SilenceSecs),
		"-f", "null", "-").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg loudness: %w", err)
	}
	res := &Result{}
	if res.LUFS, err = num(reI, out); err != nil {
		return nil, fmt.Errorf("ffmpeg loudness: no result")
	}
	res.LRA, _ = num(reLRA, out)
	res.TruePeak, _ = num(rePeak, out)
	var start float64 = -1
	for _, m := range reSilence.FindAllSubmatch(out, -1) {
		v, _ := strconv.ParseFloat(string(m[2]), 64)
		if string(m[1]) == "start" {
			start = max(0, v)
		} else if start >= 0 {
			res.Silences = append(res.Silences, Range{start, v})
			start = -1
		}
	}
	if start >= 0 && duration-start >= SilenceSecs {
		res.Silences = append(res.Silences, Range{start, duration})
	}
	c := <-clipCh
	if c.err != nil {
		return nil, c.err
	}
	res.Clipping = c.r
	return res, nil
}

// num reads the last match (the summary comes last).
func num(re *regexp.Regexp, out []byte) (float64, error) {
	m := re.FindAllSubmatch(out, -1)
	if len(m) == 0 {
		return 0, errors.New("not found")
	}
	s := string(m[len(m)-1][1])
	if s == "-inf" {
		return -120, nil // pure silence; JSON has no -inf
	}
	return strconv.ParseFloat(s, 64)
}

// clipping decodes the audio as float samples and finds runs of
// ClipRun or more full-scale samples in any channel.
func clipping(ctx context.Context, ffmpeg, path string) ([]Range, error) {
	const rate = 48000
	ch, err := channels(ctx, path)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-i", path,
		"-map", "0:a:0", "-ar", strconv.Itoa(rate), "-f", "f32le", "-acodec", "pcm_f32le", "-")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(pipe, 1<<20)
	run := make([]int, ch)
	var out []Range
	buf := make([]byte, 4*ch*4096)
	var n int64 // frames read
	for {
		k, err := io.ReadFull(r, buf)
		k -= k % (4 * ch)
		for off := 0; off < k; off += 4 * ch {
			for c := range ch {
				v := math.Float32frombits(binary.LittleEndian.Uint32(buf[off+4*c:]))
				if v >= ClipLevel || v <= -ClipLevel {
					run[c]++
					if run[c] == ClipRun {
						t := float64(n) / rate
						if l := len(out); l > 0 && t-out[l-1].End <= clipJoinSecs {
							out[l-1].End = t
						} else {
							out = append(out, Range{t, t})
						}
					}
				} else {
					run[c] = 0
				}
			}
			n++
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg audio: %w", err)
	}
	return out, nil
}

func channels(ctx context.Context, path string) (int, error) {
	ffprobe, err := media.Tool("ffprobe")
	if err != nil {
		return 0, err
	}
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=channels", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	n, err := strconv.Atoi(string(bytes.TrimSpace(out)))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("ffprobe: bad channel count")
	}
	return n, nil
}
