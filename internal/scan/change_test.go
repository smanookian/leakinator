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
	if full, _ := d.next(frame()); !full {
		t.Fatal("first frame must be read whole")
	}
}

func TestDetectorNoChangeNoRead(t *testing.T) {
	var d detector
	d.next(text(frame(), 20, 20, 30))
	// Compression noise: small pixel changes everywhere.
	noisy := text(frame(), 20, 20, 30)
	for i := range noisy.Pix {
		noisy.Pix[i] += uint8(i % 7)
	}
	if full, bands := d.next(noisy); full || len(bands) != 0 {
		t.Fatalf("got full=%v bands=%v", full, bands)
	}
}

func TestDetectorNewLineReadAtOnce(t *testing.T) {
	var d detector
	d.next(frame())
	// A pasted key: a 50-character line appears at y=200.
	full, bands := d.next(text(frame(), 20, 200, 50))
	if full || len(bands) != 1 {
		t.Fatalf("got full=%v bands=%v", full, bands)
	}
	if b := bands[0]; b.y0 > 200 || b.y1 < 214 {
		t.Fatalf("strip %v does not cover the line at 200-214", b)
	}
	// Read once; the same screen again needs no read.
	if full, bands := d.next(text(frame(), 20, 200, 50)); full || len(bands) != 0 {
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
	d.next(page1)
	if full, _ := d.next(page2); !full {
		t.Fatal("new page must be read whole")
	}
}

// A small change that keeps moving (mouse pointer) waits until it stops,
// but never longer than maxWait frames.
func TestDetectorMovingPointerWaits(t *testing.T) {
	var d detector
	d.next(frame())
	pointer := func(x int) *image.Gray { return fill(frame(), image.Rect(x, 100, x+12, 118), 255) }
	reads := 0
	for i := range maxWait - 1 {
		if _, bands := d.next(pointer(100 + 40*i)); len(bands) > 0 {
			reads++
		}
	}
	if reads != 0 {
		t.Fatalf("moving pointer read %d times before maxWait", reads)
	}
	if _, bands := d.next(pointer(100 + 40*maxWait)); len(bands) == 0 {
		t.Fatal("a change that never stops must be read after maxWait frames")
	}
}

func TestDetectorSettledSmallChangeIsRead(t *testing.T) {
	var d detector
	d.next(frame())
	typed := func() *image.Gray { return text(frame(), 20, 100, 3) } // 3 typed letters
	if _, bands := d.next(typed()); len(bands) != 0 {
		t.Fatal("small change is read only when it stops changing")
	}
	if _, bands := d.next(typed()); len(bands) != 1 {
		t.Fatal("small change that stopped must be read")
	}
}

// A blinking cursor goes back to what was read: no reads at all.
func TestDetectorBlinkingCursorNoReads(t *testing.T) {
	var d detector
	on := func() *image.Gray { return fill(text(frame(), 20, 100, 20), image.Rect(300, 100, 303, 118), 255) }
	off := func() *image.Gray { return text(frame(), 20, 100, 20) }
	d.next(on())
	for i := range 20 {
		img := off()
		if i%2 == 1 {
			img = on()
		}
		if full, bands := d.next(img); full || len(bands) != 0 {
			t.Fatalf("frame %d: blinking cursor caused a read", i)
		}
	}
}
