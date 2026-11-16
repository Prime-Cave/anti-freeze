// Package tray draws the menu bar icon and applies it to the status item.
//
// The icon is a 16×12 pt rounded rectangle with an inner fill bar:
//
//	ok:       monochrome template image, short bar at the bottom, width = memory used
//	tight:    amber outline, full-height bar (shape changes, not just colour)
//	critical: solid red shape with a white exclamation mark
//
// Rendering is pure Go so it can be unit tested without AppKit.
package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Level is the icon's state. It mirrors analyzer.State without importing it.
type Level int

const (
	LevelOK Level = iota
	LevelTight
	LevelCritical
)

const (
	scale  = 2.0  // render at @2x
	canvW  = 20.0 // points
	canvH  = 16.0
	rectX0 = 2.0
	rectY0 = 2.0
	rectW  = 16.0
	rectH  = 12.0
	radius = 3.0
	stroke = 1.5
)

// Palette for the coloured (non-template) states.
type palette struct{ tight, critical, glyph color.NRGBA }

var (
	lightPal = palette{tight: rgb(0x9A5B00), critical: rgb(0xB3261E), glyph: rgb(0xFFFFFF)}
	darkPal  = palette{tight: rgb(0xE0A040), critical: rgb(0xB3261E), glyph: rgb(0xFFFFFF)}
)

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
}

// Icon is one rendered state: PNGs for light and dark menu bars and whether
// AppKit should treat it as a template image.
type Icon struct {
	Light, Dark []byte
	Template    bool
}

// Render draws the icon for a level and memory fill fraction (0..1).
func Render(level Level, fill float64) Icon {
	fill = math.Max(0, math.Min(1, fill))
	switch level {
	case LevelTight:
		return Icon{Light: encode(draw(level, fill, lightPal)), Dark: encode(draw(level, fill, darkPal))}
	case LevelCritical:
		return Icon{Light: encode(draw(level, fill, lightPal)), Dark: encode(draw(level, fill, darkPal))}
	default:
		img := encode(draw(LevelOK, fill, lightPal))
		return Icon{Light: img, Dark: img, Template: true}
	}
}

// sdRoundRect is the signed distance from p to a rounded rectangle.
func sdRoundRect(px, py, x0, y0, w, h, r float64) float64 {
	cx, cy := x0+w/2, y0+h/2
	qx := math.Abs(px-cx) - (w/2 - r)
	qy := math.Abs(py-cy) - (h/2 - r)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - r
}

// cover converts a signed distance (in points) to pixel coverage.
func cover(d float64) float64 {
	return math.Max(0, math.Min(1, 0.5-d*scale))
}

func draw(level Level, fill float64, pal palette) *image.NRGBA {
	W, H := int(canvW*scale), int(canvH*scale)
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	black := color.NRGBA{A: 0xFF}

	inX0, inY0 := rectX0+stroke+1, rectY0+stroke+1
	inW, inH := rectW-2*(stroke+1), rectH-2*(stroke+1)

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			px, py := (float64(x)+0.5)/scale, (float64(y)+0.5)/scale
			outer := sdRoundRect(px, py, rectX0, rectY0, rectW, rectH, radius)
			var a float64
			var c color.NRGBA
			switch level {
			case LevelCritical:
				body := cover(outer)
				// Exclamation: a vertical bar and a dot, knocked out in white.
				bar := cover(sdRoundRect(px, py, 9.1, 4.0, 1.8, 5.0, 0.9))
				dot := cover(math.Hypot(px-10.0, py-11.0) - 1.0)
				g := math.Max(bar, dot)
				a = body
				c = blend(pal.critical, pal.glyph, g)
			default:
				ring := cover(outer) - cover(outer+stroke)
				barH := 3.0
				col := black
				if level == LevelTight {
					barH = inH
					col = pal.tight
				}
				bw := math.Max(inW*fill, 1.0)
				bar := cover(sdRoundRect(px, py, inX0, inY0+inH-barH, bw, barH, 1.0))
				a = math.Max(math.Max(ring, 0), bar)
				c = col
			}
			if a <= 0 {
				continue
			}
			c.A = uint8(math.Round(a * 255))
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func blend(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: 0xFF}
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
