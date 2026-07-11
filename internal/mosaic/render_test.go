package mosaic

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// solidPNG returns PNG bytes of a c-colored w×h image.
func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRender_ProducesSquareJPEG(t *testing.T) {
	covers := [][]byte{
		solidPNG(t, 300, 200, color.RGBA{200, 0, 0, 255}), // non-square → center-cropped
		solidPNG(t, 100, 100, color.RGBA{0, 200, 0, 255}),
		solidPNG(t, 100, 100, color.RGBA{0, 0, 200, 255}),
		solidPNG(t, 100, 100, color.RGBA{200, 200, 0, 255}),
	}
	out, err := Render(Plan(4), covers)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output not a JPEG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != Canvas || b.Dy() != Canvas {
		t.Errorf("output %dx%d, want %dx%d", b.Dx(), b.Dy(), Canvas, Canvas)
	}
}

func TestRender_CorruptTileBecomesBlackNotError(t *testing.T) {
	covers := [][]byte{
		[]byte("not an image"),
		solidPNG(t, 100, 100, color.RGBA{0, 200, 0, 255}),
	}
	// n==2 → 2x2 with covers 0,1 and black 2,3; cover 0 fails to decode → black.
	if _, err := Render(Plan(2), covers); err != nil {
		t.Errorf("Render must not fail on a corrupt tile: %v", err)
	}
}
