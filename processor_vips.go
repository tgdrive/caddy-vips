//go:build vips

package caddyvips

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/davidbyttow/govips/v2/vips"
)

var vipsStartup sync.Once

type vipsImageProcessor struct{}

func newImageProcessor() imageProcessor {
	vipsStartup.Do(func() {
		vips.Startup(nil)
	})
	return vipsImageProcessor{}
}

func imageProcessorAvailable() bool { return true }

func (vipsImageProcessor) Transform(source []byte, spec imageSpec) ([]byte, string, error) {
	image, err := vips.NewImageFromBuffer(source)
	if err != nil {
		return nil, "", fmt.Errorf("caddy-vips: decode image: %w", err)
	}
	defer image.Close()

	if err := image.AutoRotate(); err != nil {
		return nil, "", fmt.Errorf("caddy-vips: auto-rotate image: %w", err)
	}
	if err := applyRotationAndFlip(image, spec); err != nil {
		return nil, "", err
	}

	width, height := spec.Width, spec.Height
	if width == 0 {
		width = max(1, image.Width()*height/image.Height())
	}
	if height == 0 {
		height = max(1, image.Height()*width/image.Width())
	}

	size := vips.SizeBoth
	if spec.WithoutEnlargement {
		size = vips.SizeDown
	}
	switch spec.Fit {
	case "cover":
		err = coverImage(image, width, height, spec.Gravity, spec.WithoutEnlargement)
	case "fill":
		if spec.WithoutEnlargement && width >= image.Width() && height >= image.Height() {
			err = nil
		} else {
			err = image.ResizeWithVScale(
				float64(width)/float64(image.Width()),
				float64(height)/float64(image.Height()),
				vips.KernelLanczos3,
			)
		}
	case "outside":
		scale := maxFloat(float64(width)/float64(image.Width()), float64(height)/float64(image.Height()))
		if spec.WithoutEnlargement && scale > 1 {
			scale = 1
		}
		err = image.Resize(scale, vips.KernelLanczos3)
	case "inside", "contain":
		err = image.ThumbnailWithSize(width, height, vips.InterestingNone, size)
	default:
		err = fmt.Errorf("unsupported fit %q", spec.Fit)
	}
	if err != nil {
		return nil, "", fmt.Errorf("caddy-vips: resize image: %w", err)
	}

	switch spec.Format {
	case "png":
		data, _, exportErr := image.ExportPng(&vips.PngExportParams{Compression: 6, StripMetadata: true})
		return data, "image/png", wrapExportError(exportErr)
	case "webp":
		data, _, exportErr := image.ExportWebp(&vips.WebpExportParams{Quality: spec.Quality, StripMetadata: true})
		return data, "image/webp", wrapExportError(exportErr)
	default:
		background := parseBackground(spec.Background)
		if err := image.Flatten(&background); err != nil {
			return nil, "", fmt.Errorf("caddy-vips: flatten image: %w", err)
		}
		data, _, exportErr := image.ExportJpeg(&vips.JpegExportParams{Quality: spec.Quality, StripMetadata: true, Interlace: true})
		return data, "image/jpeg", wrapExportError(exportErr)
	}
}

func applyRotationAndFlip(image *vips.ImageRef, spec imageSpec) error {
	angles := map[int]vips.Angle{0: vips.Angle0, 90: vips.Angle90, 180: vips.Angle180, 270: vips.Angle270}
	if spec.Rotate != 0 {
		if err := image.Rotate(angles[spec.Rotate]); err != nil {
			return fmt.Errorf("caddy-vips: rotate image: %w", err)
		}
	}
	switch spec.Flip {
	case "horizontal":
		if err := image.Flip(vips.DirectionHorizontal); err != nil {
			return fmt.Errorf("caddy-vips: flip image: %w", err)
		}
	case "vertical":
		if err := image.Flip(vips.DirectionVertical); err != nil {
			return fmt.Errorf("caddy-vips: flip image: %w", err)
		}
	}
	return nil
}

func coverImage(image *vips.ImageRef, width, height int, gravity string, withoutEnlargement bool) error {
	if gravity == "attention" || gravity == "entropy" || gravity == "center" {
		interesting := vips.InterestingCentre
		if gravity == "attention" {
			interesting = vips.InterestingAttention
		} else if gravity == "entropy" {
			interesting = vips.InterestingEntropy
		}
		size := vips.SizeBoth
		if withoutEnlargement {
			size = vips.SizeDown
		}
		return image.ThumbnailWithSize(width, height, interesting, size)
	}

	scale := maxFloat(float64(width)/float64(image.Width()), float64(height)/float64(image.Height()))
	if withoutEnlargement && scale > 1 {
		scale = 1
	}
	if err := image.Resize(scale, vips.KernelLanczos3); err != nil {
		return err
	}
	cropWidth := min(width, image.Width())
	cropHeight := min(height, image.Height())
	left := max(0, (image.Width()-cropWidth)/2)
	top := max(0, (image.Height()-cropHeight)/2)
	switch gravity {
	case "top":
		top = 0
	case "bottom":
		top = max(0, image.Height()-cropHeight)
	case "left":
		left = 0
	case "right":
		left = max(0, image.Width()-cropWidth)
	}
	return image.Crop(left, top, cropWidth, cropHeight)
}

func parseBackground(raw string) vips.Color {
	if len(raw) != 6 {
		return vips.Color{R: 255, G: 255, B: 255}
	}
	r, _ := strconv.ParseUint(raw[0:2], 16, 8)
	g, _ := strconv.ParseUint(raw[2:4], 16, 8)
	b, _ := strconv.ParseUint(raw[4:6], 16, 8)
	return vips.Color{R: uint8(r), G: uint8(g), B: uint8(b)}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func wrapExportError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("caddy-vips: encode image: %w", err)
}
