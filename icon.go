package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

func GenerateCircleIcon(hexColor string) []byte {
	c := parseHexColor(hexColor)
	size := 32
	radius := float64(size) / 2.0

	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Draw anti-aliased circle with proper alpha
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) + 0.5 - radius
			dy := float64(y) + 0.5 - radius
			dist := math.Sqrt(dx*dx+dy*dy) - radius + 0.5

			var alpha uint8
			switch {
			case dist <= -0.5:
				alpha = 255
			case dist >= 0.5:
				alpha = 0
			default:
				alpha = uint8(255 * (0.5 - dist))
			}

			img.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func parseHexColor(s string) color.RGBA {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	var r, g, b uint8
	if len(s) == 6 {
		r = hexToByte(s[0:2])
		g = hexToByte(s[2:4])
		b = hexToByte(s[4:6])
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func hexToByte(s string) uint8 {
	var val uint8
	for _, c := range s {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val += uint8(c - '0')
		case c >= 'a' && c <= 'f':
			val += uint8(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			val += uint8(c - 'A' + 10)
		}
	}
	return val
}
