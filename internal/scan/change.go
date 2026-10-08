package scan

import "image"

// detector finds what changed on screen since the last OCR.
//
// The frame is split into 16×16 blocks. A block changed when enough of its
// pixels moved a lot (text appears, disappears or changes). Small noise
// from video compression does not count. Changes are compared with the
// last frame that was read, so slow typing adds up until it is noticed.
//
// Big changes (new page, scrolling, pasted text) are read at once.
// Small changes (mouse pointer, typing) are read when they stop moving,
// or after maxWait frames if they never stop.
type detector struct {
	ref, prev *image.Gray
	counts    []int32 // changed pixels per block, against ref
	rowWait   []int   // frames a changed block row has waited
	read      []bool  // block rows to read now
}

const (
	block       = 16
	pixDiff     = 40   // a pixel changed if it moved more than this (0-255)
	minPixels   = 6    // a block changed if at least this many pixels changed
	smallBlocks = 40   // this many changed blocks or fewer is a small change
	maxWait     = 4    // read a small change after this many frames anyway
	fullBlocks  = 0.30 // read the whole frame if this share of blocks changed
	fullHeight  = 0.50 // or if the changed strips cover this share of the height
)

// band is a strip of rows [y0, y1) that changed.
type band struct{ y0, y1 int }

// next compares img with the last read frame. It returns full=true when
// the whole frame should be read, else the strips to read (maybe none).
// The caller must read what it returns; next updates its reference.
// img must be a compact image (Stride == width).
func (d *detector) next(img *image.Gray) (full bool, bands []band) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bw, bh := (w+block-1)/block, (h+block-1)/block
	if d.ref == nil {
		d.ref = clone(img)
		d.prev = clone(img)
		d.counts = make([]int32, bw*bh)
		d.rowWait = make([]int, bh)
		d.read = make([]bool, bh)
		return true, nil
	}
	defer copy(d.prev.Pix, img.Pix)

	diffBlocks(d.counts, img, d.ref, bw)
	nChanged := 0
	for _, c := range d.counts {
		if c >= minPixels {
			nChanged++
		}
	}
	if nChanged == 0 {
		clear(d.rowWait)
		return false, nil
	}
	if float64(nChanged) > fullBlocks*float64(bw*bh) {
		copy(d.ref.Pix, img.Pix)
		clear(d.rowWait)
		return true, nil
	}

	small := nChanged <= smallBlocks
	clear(d.read)
	for by := range bh {
		changed, moving := false, false
		for bx := range bw {
			if d.counts[by*bw+bx] < minPixels {
				continue
			}
			changed = true
			if small && !moving && blockDiff(img, d.prev, bx, by) >= minPixels {
				moving = true
			}
		}
		switch {
		case !changed:
			d.rowWait[by] = 0
		case !small || !moving || d.rowWait[by]+1 >= maxWait:
			d.read[by] = true
			d.rowWait[by] = 0
		default:
			d.rowWait[by]++
		}
	}

	// Join block rows into strips; a gap of one block row is joined.
	total := 0
	for by := 0; by < bh; {
		if !d.read[by] {
			by++
			continue
		}
		start, end := by, by
		for by < bh && (d.read[by] || (by+1 < bh && d.read[by+1])) {
			if d.read[by] {
				end = by
			}
			by++
		}
		b := band{start * block, min(h, (end+1)*block)}
		bands = append(bands, b)
		total += b.y1 - b.y0
	}
	if float64(total) > fullHeight*float64(h) {
		copy(d.ref.Pix, img.Pix)
		clear(d.rowWait)
		return true, nil
	}
	for _, b := range bands {
		copy(d.ref.Pix[b.y0*w:b.y1*w], img.Pix[b.y0*w:b.y1*w])
	}
	return false, bands
}

func clone(img *image.Gray) *image.Gray {
	c := image.NewGray(img.Rect)
	copy(c.Pix, img.Pix)
	return c
}

// diffBlocks counts, per block, the pixels that differ a lot between a and b.
func diffBlocks(counts []int32, a, b *image.Gray, bw int) {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	clear(counts)
	for y := range h {
		ra := a.Pix[y*w : (y+1)*w]
		rb := b.Pix[y*w : (y+1)*w]
		row := counts[(y/block)*bw:]
		for x, c := range ra {
			if d := int(c) - int(rb[x]); d > pixDiff || d < -pixDiff {
				row[x/block]++
			}
		}
	}
}

// blockDiff counts the pixels of one block that differ a lot between a and b.
func blockDiff(a, b *image.Gray, bx, by int) int32 {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	var n int32
	for y := by * block; y < min(h, (by+1)*block); y++ {
		for x := bx * block; x < min(w, (bx+1)*block); x++ {
			if d := int(a.Pix[y*w+x]) - int(b.Pix[y*w+x]); d > pixDiff || d < -pixDiff {
				n++
			}
		}
	}
	return n
}
