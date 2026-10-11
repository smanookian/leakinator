package scan

import (
	"image"
	"sort"
)

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
	shifted   []int32 // the same, against ref moved by a scroll
	rowWait   []int   // frames a changed block row has waited
	read      []bool  // block rows to read now
}

const (
	block       = 16
	pixDiff     = 40   // a pixel changed if it moved more than this (0-255)
	minInk      = 3    // a block holds text if this many pixels differ from the background
	minPixels   = 6    // a block changed if at least this many pixels changed
	smallBlocks = 40   // this many changed blocks or fewer is a small change
	maxWait     = 4    // read a small change after this many frames anyway
	fullBlocks  = 0.30 // read the whole frame if this share of blocks changed
	fullHeight  = 0.50 // or if the changed strips cover this share of the height
)

// band is a strip of rows [y0, y1) that changed.
type band struct{ y0, y1 int }

// change says what to do with a frame.
type change struct {
	full  bool   // read the whole frame
	bands []band // else read these strips
	// Scrolling: text in the blocks marked in moved (in new-frame
	// positions, bw blocks per row) moved by dy pixels. It needs no new
	// read. changed marks the blocks whose content changed in place.
	dy      int
	moved   []bool
	changed []bool
	bw      int
}

// next compares img with the last read frame and says what to read.
// The caller must read what it returns; next updates its reference.
// img must be a compact image (Stride == width).
func (d *detector) next(img *image.Gray) change {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bw, bh := (w+block-1)/block, (h+block-1)/block
	if d.ref == nil {
		d.ref = clone(img)
		d.prev = clone(img)
		d.counts = make([]int32, bw*bh)
		d.shifted = make([]int32, bw*bh)
		d.rowWait = make([]int, bh)
		d.read = make([]bool, bh)
		return change{full: true}
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
		return change{}
	}
	if nChanged > smallBlocks {
		if c, ok := d.scroll(img, bw, bh, nChanged); ok {
			return c
		}
	}
	if float64(nChanged) > fullBlocks*float64(bw*bh) {
		return d.fullRead(img)
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
	bands, total := d.joinRows(h)
	if float64(total) > fullHeight*float64(h) {
		return d.fullRead(img)
	}
	for _, b := range bands {
		copy(d.ref.Pix[b.y0*w:b.y1*w], img.Pix[b.y0*w:b.y1*w])
	}
	return change{bands: bands}
}

func (d *detector) fullRead(img *image.Gray) change {
	copy(d.ref.Pix, img.Pix)
	clear(d.rowWait)
	return change{full: true}
}

// joinRows turns the block rows marked in d.read into strips;
// a gap of one block row is joined.
func (d *detector) joinRows(h int) (bands []band, total int) {
	bh := len(d.read)
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
	return bands, total
}

// scroll checks if the change is mostly text moving up or down. If so,
// only the blocks that the move does not explain (new lines coming into
// view, a status bar that changed) need reading.
func (d *detector) scroll(img *image.Gray, bw, bh, nChanged int) (change, bool) {
	h := img.Rect.Dy()
	ink := inkBlocks(img, bw)
	bestDy, bestMoved := 0, 0
	for _, dy := range shiftCandidates(d.ref, img, d.counts, bw, 8) {
		shiftedDiffBlocks(d.shifted, img, d.ref, bw, dy)
		moved := 0
		for i, c := range d.counts {
			// Moved: changed in place, same as ref after the move, and
			// holds text (blank matches blank at any shift).
			if c >= minPixels && d.shifted[i] < minPixels && ink[i] >= minInk {
				moved++
			}
		}
		if moved > bestMoved {
			bestDy, bestMoved = dy, moved
		}
	}
	// The move must explain most of the change.
	if bestMoved*2 < nChanged {
		return change{}, false
	}
	shiftedDiffBlocks(d.shifted, img, d.ref, bw, bestDy)
	moved := make([]bool, bw*bh)
	changed := make([]bool, bw*bh)
	clear(d.read)
	for i, c := range d.counts {
		if c < minPixels {
			continue
		}
		changed[i] = true
		switch {
		case d.shifted[i] >= minPixels:
			d.read[i/bw] = true // the move does not explain this block
		case ink[i] >= minInk:
			moved[i] = true // text that moved here
		}
	}
	bands, total := d.joinRows(h)
	if float64(total) > fullHeight*float64(h) {
		return d.fullRead(img), true
	}
	copy(d.ref.Pix, img.Pix)
	clear(d.rowWait)
	return change{bands: bands, dy: bestDy, moved: moved, changed: changed, bw: bw}, true
}

// inkBlocks counts, per block, the pixels that differ a lot from the
// background (the most common gray value).
func inkBlocks(img *image.Gray, bw int) []int32 {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var hist [256]int
	for _, p := range img.Pix {
		hist[p]++
	}
	bg := 0
	for v, n := range hist {
		if n > hist[bg] {
			bg = v
		}
	}
	out := make([]int32, bw*((h+block-1)/block))
	for y := range h {
		row := out[(y/block)*bw:]
		for x, p := range img.Pix[y*w : (y+1)*w] {
			if d := int(p) - bg; d > pixDiff || d < -pixDiff {
				row[x/block]++
			}
		}
	}
	return out
}

const segments = 16 // a row's signature: its brightness in 16 parts

// shiftCandidates returns the n most likely vertical moves (img row y
// shows ref row y-dy), found by comparing row signatures of the rows that
// changed. The caller checks them.
func shiftCandidates(ref, img *image.Gray, counts []int32, bw, n int) []int {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	sig := func(g *image.Gray) []int32 {
		s := make([]int32, h*segments)
		for y := range h {
			for x, p := range g.Pix[y*w : (y+1)*w] {
				s[y*segments+x*segments/w] += int32(p)
			}
		}
		return s
	}
	sr, si := sig(ref), sig(img)
	var rowsUsed []int
	for y := range h {
		by := y / block
		for bx := range bw {
			if counts[by*bw+bx] >= minPixels {
				rowsUsed = append(rowsUsed, y)
				break
			}
		}
	}
	type cand struct {
		dy   int
		cost float64
	}
	var cs []cand
	for dy := -h / 3; dy <= h/3; dy++ {
		if dy == 0 {
			continue
		}
		var sum int64
		used := 0
		for _, y := range rowsUsed {
			sy := y - dy
			if sy < 0 || sy >= h {
				continue
			}
			a, b := si[y*segments:(y+1)*segments], sr[sy*segments:(sy+1)*segments]
			for k := range segments {
				d := int64(a[k] - b[k])
				if d < 0 {
					d = -d
				}
				sum += d
			}
			used++
		}
		if used*2 < len(rowsUsed) {
			continue // too little overlap to trust
		}
		cs = append(cs, cand{dy, float64(sum) / float64(used)})
	}
	// Keep local minima only: -40, -39 and -41 are one candidate.
	minima := cs[:0:0]
	for i, c := range cs {
		if (i == 0 || cs[i-1].dy != c.dy-1 || cs[i-1].cost >= c.cost) &&
			(i == len(cs)-1 || cs[i+1].dy != c.dy+1 || cs[i+1].cost > c.cost) {
			minima = append(minima, c)
		}
	}
	cs = minima
	sort.Slice(cs, func(i, j int) bool { return cs[i].cost < cs[j].cost })
	out := make([]int, 0, n)
	for _, c := range cs[:min(n, len(cs))] {
		out = append(out, c.dy)
	}
	return out
}

// shiftedDiffBlocks is diffBlocks against ref moved down by dy rows.
// Rows with no source in ref count as changed.
func shiftedDiffBlocks(counts []int32, a, ref *image.Gray, bw, dy int) {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	clear(counts)
	for y := range h {
		row := counts[(y/block)*bw:]
		sy := y - dy
		if sy < 0 || sy >= h {
			for x := 0; x < w; x += block {
				row[x/block] += int32(min(block, w-x))
			}
			continue
		}
		ra := a.Pix[y*w : (y+1)*w]
		rb := ref.Pix[sy*w : (sy+1)*w]
		for x, c := range ra {
			if d := int(c) - int(rb[x]); d > pixDiff || d < -pixDiff {
				row[x/block]++
			}
		}
	}
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
