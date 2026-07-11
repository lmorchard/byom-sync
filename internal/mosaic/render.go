package mosaic

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg" // named: provides Encode and registers the JPEG decoder

	// Decoders for the other formats artstore may have saved.
	_ "image/gif"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const jpegQuality = 88

// Render composites the ranked cover bytes onto a black square canvas per l and
// returns JPEG bytes. covers is index-aligned to Slot.CoverIndex. A slot whose
// cover is missing, out of range, or undecodable is left black — Render never
// fails on bad art (only on JPEG encoding, which shouldn't happen for RGBA).
func Render(l Layout, covers [][]byte) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, l.Canvas, l.Canvas))
	// image.Black is a predefined *image.Uniform, usable directly as the src.
	draw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, draw.Src)

	for _, s := range l.Slots {
		if s.CoverIndex < 0 || s.CoverIndex >= len(covers) {
			continue // black
		}
		src, _, err := image.Decode(bytes.NewReader(covers[s.CoverIndex]))
		if err != nil {
			continue // black
		}
		sq := centerSquare(src)
		r := image.Rect(s.Rect.X, s.Rect.Y, s.Rect.X+s.Rect.W, s.Rect.Y+s.Rect.H)
		xdraw.CatmullRom.Scale(dst, r, sq, sq.Bounds(), xdraw.Over, nil)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// centerSquare returns the largest centered square sub-image of src.
func centerSquare(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h < side {
		side = h
	}
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	crop := image.Rect(x0, y0, x0+side, y0+side)
	if si, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return si.SubImage(crop)
	}
	// Fallback: copy the crop region into a fresh RGBA.
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(out, out.Bounds(), src, crop.Min, draw.Src)
	return out
}
