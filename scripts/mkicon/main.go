// Command mkicon renders Headroom's app icon (.icns) from code, so no binary
// image assets live in the repo. It draws a slate squircle with the runway bar:
// memory on the left, disk on the right, hatched overflow across the gap.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	out := flag.String("out", "icons.icns", "output .icns path")
	pngOut := flag.String("png", "", "optional: also write a 512px PNG here")
	flag.Parse()

	dir, err := os.MkdirTemp("", "headroom-iconset-*")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	set := filepath.Join(dir, "icons.iconset")
	if err := os.Mkdir(set, 0o700); err != nil {
		log.Fatal(err)
	}
	for _, s := range []int{16, 32, 128, 256, 512} {
		write(filepath.Join(set, fmt.Sprintf("icon_%dx%d.png", s, s)), render(s))
		write(filepath.Join(set, fmt.Sprintf("icon_%dx%d@2x.png", s, s)), render(s*2))
	}
	if *pngOut != "" {
		write(*pngOut, render(512))
	}
	// Fixed arguments only; no shell.
	cmd := exec.Command("/usr/bin/iconutil", "-c", "icns", "-o", *out, set) // #nosec G204 -- fixed binary, paths from the Makefile
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func write(path string, img image.Image) {
	f, err := os.Create(path) // #nosec G304 -- build script writing to the path the Makefile gives
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func hex(v uint32) color.NRGBA {
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

func sdBox(px, py, x0, y0, w, h, r float64) float64 {
	cx, cy := x0+w/2, y0+h/2
	qx := math.Abs(px-cx) - (w/2 - r)
	qy := math.Abs(py-cy) - (h/2 - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

func render(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	px1 := 1 / s // one pixel in unit space
	bgTop, bgBot := hex(0x2E3B4E), hex(0x17202B)
	memUsed, memSq, memSpare := hex(0xC3CCD8), hex(0x7F8A99), hex(0x3A434F)
	diskFree := hex(0x232A33)
	swapA, swapB := hex(0xE0A040), hex(0x9A5B00)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			u, v := (float64(x)+0.5)/s, (float64(y)+0.5)/s
			// macOS icon grid: 824/1024 body, centred.
			body := sdBox(u, v, 0.1, 0.1, 0.8, 0.8, 0.18)
			a := clamp(0.5 - body/px1)
			if a <= 0 {
				continue
			}
			c := lerp(bgTop, bgBot, v)
			// Runway bar.
			bx0, by0, bw, bh := 0.2, 0.44, 0.6, 0.12
			if sdBox(u, v, bx0, by0, bw, bh, 0.03) < 0 {
				t := (u - bx0) / bw
				switch {
				case t < 0.30:
					c = memUsed
				case t < 0.42:
					c = memSq
				case t < 0.55:
					c = memSpare
				case t < 0.75:
					// hatched overflow at 135deg
					if int(math.Floor((u+v)*s/(s*0.035)))%2 == 0 {
						c = swapA
					} else {
						c = swapB
					}
				default:
					c = diskFree
				}
			}
			// Caption ticks above each half.
			if sdBox(u, v, 0.2, 0.36, 0.2, 0.025, 0.012) < 0 || sdBox(u, v, 0.56, 0.36, 0.14, 0.025, 0.012) < 0 {
				c = memSq
			}
			c.A = uint8(a * 255)
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func clamp(x float64) float64 { return math.Max(0, math.Min(1, x)) }

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.NRGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}
