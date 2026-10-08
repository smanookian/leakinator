// Package ocr reads text from images with the tesseract program.
package ocr

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/smanookian/leakinator/internal/media"
)

// Word is one word with its box in image pixels.
type Word struct {
	Text string
	Box  image.Rectangle
	Conf float64 // 0-100
}

// Line is a line of words, left to right.
type Line struct {
	Words []Word
	Box   image.Rectangle
}

// Text joins the words with single spaces. offs[i] is where word i starts.
func (l Line) Text() (text string, offs []int) {
	var b strings.Builder
	offs = make([]int, len(l.Words))
	for i, w := range l.Words {
		if i > 0 {
			b.WriteByte(' ')
		}
		offs[i] = b.Len()
		b.WriteString(w.Text)
	}
	return b.String(), offs
}

// Mode is the tesseract page layout mode.
type Mode int

const (
	Page  Mode = 3 // full frame: find columns and blocks
	Block Mode = 6 // a strip of lines
)

// Reader runs tesseract.
type Reader struct {
	bin string
}

// New finds tesseract and checks that English is installed.
func New() (*Reader, error) {
	bin, err := media.Tool("tesseract")
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(bin, "--list-langs").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tesseract does not run: %w", err)
	}
	if !bytes.Contains(out, []byte("\neng")) {
		return nil, fmt.Errorf("tesseract has no English data (install tesseract-data-eng or tesseract-ocr-eng)")
	}
	return &Reader{bin: bin}, nil
}

// Read returns the lines of text in img. Light text on a dark background
// is inverted first; tesseract reads dark text on light best.
func (r *Reader) Read(ctx context.Context, img *image.Gray, mode Mode) ([]Line, error) {
	in := pgm(img, isDark(img))
	cmd := exec.CommandContext(ctx, r.bin, "stdin", "stdout", "-l", "eng", "--psm", strconv.Itoa(int(mode)),
		"-c", "tessedit_do_invert=0", "tsv")
	cmd.Stdin = bytes.NewReader(in)
	// One thread per call; we run one call per CPU core instead.
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tesseract: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	off := img.Rect.Min
	return parseTSV(out, off), nil
}

func isDark(img *image.Gray) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var sum uint64
	for y := range h {
		for _, p := range img.Pix[y*img.Stride : y*img.Stride+w] {
			sum += uint64(p)
		}
	}
	return sum < uint64(w*h)*128
}

// pgm encodes img as binary PGM, optionally inverted.
func pgm(img *image.Gray, invert bool) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	hdr := fmt.Sprintf("P5\n%d %d\n255\n", w, h)
	buf := make([]byte, len(hdr)+w*h)
	n := copy(buf, hdr)
	for y := range h {
		row := img.Pix[y*img.Stride : y*img.Stride+w]
		dst := buf[n+y*w : n+(y+1)*w]
		if invert {
			for x, p := range row {
				dst[x] = 255 - p
			}
		} else {
			copy(dst, row)
		}
	}
	return buf
}

// parseTSV reads tesseract TSV output. Boxes are moved by off.
func parseTSV(data []byte, off image.Point) []Line {
	type key struct{ block, par, line int }
	var lines []Line
	idx := map[key]int{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 12 || f[0] != "5" {
			continue
		}
		text := strings.TrimSpace(f[11])
		if text == "" {
			continue
		}
		n := make([]int, 10)
		for i := range 10 {
			n[i], _ = strconv.Atoi(f[i])
		}
		conf, _ := strconv.ParseFloat(f[10], 64)
		box := image.Rect(n[6], n[7], n[6]+n[8], n[7]+n[9]).Add(off)
		k := key{n[2], n[3], n[4]}
		i, ok := idx[k]
		if !ok {
			i = len(lines)
			idx[k] = i
			lines = append(lines, Line{Box: box})
		}
		lines[i].Words = append(lines[i].Words, Word{Text: text, Box: box, Conf: conf})
		lines[i].Box = lines[i].Box.Union(box)
	}
	return lines
}
