package scan

import (
	"image"
	"testing"
)

const tw, th = 640, 360

func frame() *image.Gray {
	img := image.NewGray(image.Rect(0, 0, tw, th))
	for i := range img.Pix {
		img.Pix[i] = 30 // dark editor background
	}
	return img
}

func fill(img *image.Gray, r image.Rectangle, v uint8) *image.Gray {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.Pix[y*tw+x] = v
		}
	}
	return img
}

// text draws a fake line of text: bright strokes on dark.
func text(img *image.Gray, x0, y0, chars int) *image.Gray {
	for c := range chars {
		fill(img, image.Rect(x0+c*10+2, y0, x0+c*10+5, y0+14), 220)
	}
	return img
}

func TestDetectorFirstFrameIsFull(t *testing.T) {
	var d detector
	if full, _ := d.fb(frame()); !full {
		t.Fatal("first frame must be read whole")
	}
}

func TestDetectorNoChangeNoRead(t *testing.T) {
	var d detector
	d.fb(text(frame(), 20, 20, 30))
	// Compression noise: small pixel changes everywhere.
	noisy := text(frame(), 20, 20, 30)
	for i := range noisy.Pix {
		noisy.Pix[i] += uint8(i % 7)
	}
	if full, bands := d.fb(noisy); full || len(bands) != 0 {
		t.Fatalf("got full=%v bands=%v", full, bands)
	}
}

func TestDetectorNewLineReadAtOnce(t *testing.T) {
	var d detector
	d.fb(frame())
	// A pasted key: a 50-character line appears at y=200.
	full, bands := d.fb(text(frame(), 20, 200, 50))
	if full || len(bands) != 1 {
		t.Fatalf("got full=%v bands=%v", full, bands)
	}
	if b := bands[0]; b.y0 > 200 || b.y1 < 214 {
		t.Fatalf("strip %v does not cover the line at 200-214", b)
	}
	// Read once; the same screen again needs no read.
	if full, bands := d.fb(text(frame(), 20, 200, 50)); full || len(bands) != 0 {
		t.Fatalf("second time: full=%v bands=%v", full, bands)
	}
}

func TestDetectorPageChangeIsFull(t *testing.T) {
	var d detector
	page1, page2 := frame(), frame()
	for y := 0; y < th-20; y += 20 {
		text(page1, 10, y, 60)
		text(page2, 15, y+5, 55)
	}
	d.fb(page1)
	if full, _ := d.fb(page2); !full {
		t.Fatal("new page must be read whole")
	}
}

// A small change that keeps moving (mouse pointer) waits until it stops,
// but never longer than maxWait frames.
func TestDetectorMovingPointerWaits(t *testing.T) {
	var d detector
	d.fb(frame())
	pointer := func(x int) *image.Gray { return fill(frame(), image.Rect(x, 100, x+12, 118), 255) }
	reads := 0
	for i := range maxWait - 1 {
		if _, bands := d.fb(pointer(100 + 40*i)); len(bands) > 0 {
			reads++
		}
	}
	if reads != 0 {
		t.Fatalf("moving pointer read %d times before maxWait", reads)
	}
	if _, bands := d.fb(pointer(100 + 40*maxWait)); len(bands) == 0 {
		t.Fatal("a change that never stops must be read after maxWait frames")
	}
}

func TestDetectorSettledSmallChangeIsRead(t *testing.T) {
	var d detector
	d.fb(frame())
	typed := func() *image.Gray { return text(frame(), 20, 100, 3) } // 3 typed letters
	if _, bands := d.fb(typed()); len(bands) != 0 {
		t.Fatal("small change is read only when it stops changing")
	}
	if _, bands := d.fb(typed()); len(bands) != 1 {
		t.Fatal("small change that stopped must be read")
	}
}

// A blinking cursor goes back to what was read: no reads at all.
func TestDetectorBlinkingCursorNoReads(t *testing.T) {
	var d detector
	on := func() *image.Gray { return fill(text(frame(), 20, 100, 20), image.Rect(300, 100, 303, 118), 255) }
	off := func() *image.Gray { return text(frame(), 20, 100, 20) }
	d.fb(on())
	for i := range 20 {
		img := off()
		if i%2 == 1 {
			img = on()
		}
		if full, bands := d.fb(img); full || len(bands) != 0 {
			t.Fatalf("frame %d: blinking cursor caused a read", i)
		}
	}
}

func (d *detector) fb(img *image.Gray) (bool, []band) {
	c := d.next(img)
	return c.full, c.bands
}

// page draws lines of different lengths from y0 on, every 20 px, in the
// editor area above the status bar (clipped like a real screen).
func page(img *image.Gray, y0 int) *image.Gray {
	area := image.Rect(0, 0, tw, th-40)
	for i := range 20 {
		y := y0 + i*20
		for c := range 20 + (i*7)%40 {
			r := image.Rect(20+c*10+2, y, 20+c*10+5, y+14).Intersect(area)
			fill(img, r, 220)
		}
	}
	return img
}

// Scrolling: the move is found, only new lines are read.
func TestDetectorScroll(t *testing.T) {
	var d detector
	bar := func(img *image.Gray, chars int) *image.Gray { return text(img, 20, th-30, chars) } // fixed status bar
	d.next(bar(page(frame(), 10), 10))
	c := d.next(bar(page(frame(), -30), 10)) // scrolled up 40 px
	if c.full || c.dy != -40 {
		t.Fatalf("got full=%v dy=%d, want dy=-40", c.full, c.dy)
	}
	for _, b := range c.bands {
		if b.y1 <= th-40-60 {
			t.Fatalf("read strip %v: old lines that only moved must not be read", b)
		}
	}
	// The status bar changes while scrolling: it is read.
	c = d.next(bar(page(frame(), -70), 25))
	if c.dy != -40 {
		t.Fatalf("second scroll: dy=%d", c.dy)
	}
	barRead := false
	for _, b := range c.bands {
		if b.y0 <= th-30 && b.y1 >= th-16 {
			barRead = true
		}
	}
	if !barRead {
		t.Fatalf("changed status bar not read: %v", c.bands)
	}
}
