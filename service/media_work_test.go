package service

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func TestMediaWorkThumbnailPreservesOriginalAndTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 1024, 256))
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	unchanged := append([]byte(nil), original.Bytes()...)
	preview, err := mediaWorkThumbnail(original.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(preview))
	if err != nil || format != "jpeg" || decoded.Bounds().Dx() != 512 || decoded.Bounds().Dy() != 128 {
		t.Fatal("thumbnail aspect ratio or encoding changed")
	}
	r, g, b, _ := decoded.At(0, 0).RGBA()
	if r < 64000 || g < 64000 || b < 64000 {
		t.Fatal("transparent cover became black")
	}
	if !bytes.Equal(unchanged, original.Bytes()) {
		t.Fatal("original was modified")
	}
	if _, err := mediaWorkThumbnail([]byte("not an image")); err == nil {
		t.Fatal("malformed source admitted")
	}
}

func TestAnimatedGIFGetsStaticCoverWithoutChangingAnimation(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	a := image.NewPaletted(image.Rect(0, 0, 16, 8), palette)
	b := image.NewPaletted(image.Rect(0, 0, 16, 8), palette)
	b.Pix[0] = 1
	var original bytes.Buffer
	if err := gif.EncodeAll(&original, &gif.GIF{Image: []*image.Paletted{a, b}, Delay: []int{4, 8}}); err != nil {
		t.Fatal(err)
	}
	preview, err := mediaWorkThumbnail(original.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(preview)); err != nil || format != "jpeg" {
		t.Fatal("GIF cover is not static JPEG")
	}
	animation, err := gif.DecodeAll(bytes.NewReader(original.Bytes()))
	if err != nil || len(animation.Image) != 2 || animation.Delay[1] != 8 {
		t.Fatal("GIF animation was damaged")
	}
}
