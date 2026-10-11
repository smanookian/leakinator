package scan

import (
	"image"
	"testing"

	"github.com/smanookian/leakinator/secretengine"
)

// Scrolled lines move with their secrets; fixed lines stay; lines that
// leave the screen are dropped.
func TestScrollModel(t *testing.T) {
	const bw, bh = 10, 10 // 160x160 px
	moved := make([]bool, bw*bh)
	for by := range 8 { // rows 0-127 scrolled, the bottom bar did not
		for bx := range bw {
			moved[by*bw+bx] = true
		}
	}
	key := hit{f: secretengine.Finding{Label: "KEY"}, box: image.Rect(10, 60, 50, 70)}
	model := []textLine{
		{box: image.Rect(0, 60, 100, 70), hits: []hit{key}}, // moves to 30-40
		{box: image.Rect(0, 10, 100, 20)},                   // moves off screen
		{box: image.Rect(0, 140, 100, 150)},                 // status bar, stays
	}
	got := scrollModel(model, &change{dy: -30, moved: moved, changed: moved, bw: bw}, bh*block)
	if len(got) != 2 {
		t.Fatalf("want 2 lines, got %+v", got)
	}
	if got[0].box.Min.Y != 30 || got[0].hits[0].box.Min.Y != 30 {
		t.Fatalf("line or its secret did not move: %+v", got[0])
	}
	if got[1].box.Min.Y != 140 {
		t.Fatalf("status bar moved: %+v", got[1])
	}
	if model[0].hits[0].box.Min.Y != 60 {
		t.Fatal("scrollModel changed the old model")
	}
}
