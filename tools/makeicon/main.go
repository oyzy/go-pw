// Command makeicon renders the GoKeep application icon. Everything is drawn with plain
// Go (no external image libraries) using signed distance based shapes with 4x4
// supersampling, so the icon stays crisp at every size from 16px to 1024px.
//
//	go run ./tools/makeicon
//
// It writes build/appicon.png (the source for the platform icon set) and
// frontend/dist/icon.png (favicon and lock screen logo).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const designSize = 256.0
const samples = 4

type rgb struct{ r, g, b float64 }

func (c rgb) mix(other rgb, t float64) rgb {
	t = math.Max(0, math.Min(1, t))
	return rgb{
		r: c.r + (other.r-c.r)*t,
		g: c.g + (other.g-c.g)*t,
		b: c.b + (other.b-c.b)*t,
	}
}

// The palette matches the application accent colour.
var (
	topBlue = rgb{0.38, 0.63, 1.00} // #61A1FF
	botBlue = rgb{0.15, 0.38, 0.78} // #2661C7
	white   = rgb{1, 1, 1}
	keyhole = rgb{0.14, 0.34, 0.70}
)

// --- shape helpers (all coordinates in the 256x256 design space) ---------------

func inRoundedRect(px, py, x0, y0, x1, y1, radius float64) bool {
	// Distance to the rounded rectangle: negative inside.
	cx := (x0 + x1) / 2
	cy := (y0 + y1) / 2
	hw := (x1 - x0) / 2
	hh := (y1 - y0) / 2
	d := math.Hypot(math.Max(math.Abs(px-cx)-(hw-radius), 0), math.Max(math.Abs(py-cy)-(hh-radius), 0)) - radius
	return d <= 0
}

func inCircle(px, py, cx, cy, radius float64) bool {
	return math.Hypot(px-cx, py-cy) <= radius
}

// inShackle is the open ring on top of the padlock body.
func inShackle(px, py float64) bool {
	const cx, cy, mid, half = 128.0, 116.0, 40.0, 11.5
	if py > cy+half { // keep the ring open at the bottom, the body covers the rest
		return false
	}
	d := math.Hypot(px-cx, py-cy)
	return d >= mid-half && d <= mid+half
}

func inLockBody(px, py float64) bool {
	return inRoundedRect(px, py, 68, 112, 188, 214, 24)
}

func inKeyhole(px, py float64) bool {
	if inCircle(px, py, 128, 156, 15) {
		return true
	}
	return inRoundedRect(px, py, 120, 162, 136, 190, 8)
}

// render draws the icon at the requested pixel size.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := designSize / float64(size)
	total := float64(samples * samples)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var bgCoverage, lockCoverage, keyholeCoverage float64
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/samples) * scale
					py := (float64(y) + (float64(sy)+0.5)/samples) * scale
					if inRoundedRect(px, py, 6, 6, 250, 250, 56) {
						bgCoverage++
					}
					if inLockBody(px, py) || inShackle(px, py) {
						lockCoverage++
					}
					if inKeyhole(px, py) {
						keyholeCoverage++
					}
				}
			}
			bg := bgCoverage / total
			lock := lockCoverage / total
			hole := keyholeCoverage / total
			// Vertical gradient plus a soft highlight in the upper left corner.
			gradient := topBlue.mix(botBlue, float64(y)/float64(size))
			dx := (float64(x)/float64(size) - 0.32) / 1.1
			dy := (float64(y)/float64(size) - 0.22) / 1.1
			highlight := math.Max(0, 1-math.Hypot(dx, dy))
			gradient = gradient.mix(white, 0.18*highlight*highlight)

			c := gradient.mix(white, lock)
			c = c.mix(keyhole, hole)
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(c.r * 255)),
				G: uint8(math.Round(c.g * 255)),
				B: uint8(math.Round(c.b * 255)),
				A: uint8(math.Round(bg * 255)),
			})
		}
	}
	return img
}

func writePNG(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		log.Fatalf("cannot create %s: %v", path, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		log.Fatalf("cannot write %s: %v", path, err)
	}
	log.Printf("wrote %s", path)
}

func main() {
	// build/appicon.png is the 1024px master the Wails icon generator turns into
	// build/windows/icon.ico and build/darwin/icons.icns.
	writePNG(filepath.Join("build", "appicon.png"), render(1024))
	// A small version for the user interface (favicon, lock screen).
	writePNG(filepath.Join("frontend", "dist", "icon.png"), render(256))
}
