//go:build !vips

package caddyvips

func newImageProcessor() imageProcessor { return nil }
func imageProcessorAvailable() bool     { return false }
