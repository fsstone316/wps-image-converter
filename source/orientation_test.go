package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func jpegWithOrientation(t *testing.T, orient int) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 3, 2))
	im.Set(0, 0, color.RGBA{250, 0, 0, 255})
	im.Set(2, 0, color.RGBA{0, 250, 0, 255})
	im.Set(0, 1, color.RGBA{0, 0, 250, 255})
	im.Set(2, 1, color.RGBA{250, 250, 0, 255})
	var b bytes.Buffer
	if err := jpeg.Encode(&b, im, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 26)
	copy(tiff, []byte("II"))
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x112)
	binary.LittleEndian.PutUint16(tiff[12:14], 3)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint16(tiff[18:20], uint16(orient))
	app := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:4], uint16(len(app)+2))
	seg = append(seg, app...)
	return append(append(append([]byte{}, b.Bytes()[:2]...), seg...), b.Bytes()[2:]...)
}
func TestExifAndWpsRotationNotDoubled(t *testing.T) {
	// WPS uses rot=90 for both an EXIF=6 portrait photo and an untagged photo.
	cases := []struct {
		exif         int
		rot          int64
		wantW, wantH int
	}{
		{6, 5400000, 2, 3}, {1, 5400000, 2, 3}, {1, 0, 3, 2}, {3, 5400000, 3, 2},
	}
	for _, tc := range cases {
		raw := jpegWithOrientation(t, tc.exif)
		if got := jpegEXIFOrientation(raw); got != tc.exif {
			t.Fatalf("EXIF got %d want %d", got, tc.exif)
		}
		dst, ext, dim, changed, err := normalizeWpsBytes(raw, "jpeg", tc.exif, tc.rot, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			if tc.exif != 1 || tc.rot != 0 {
				t.Fatal("skipped orientation")
			}
			continue
		}
		if ext != "jpg" || dim.W != tc.wantW || dim.H != tc.wantH {
			t.Fatalf("wrong geometry: %+v %s", dim, ext)
		}
		decoded, err := jpeg.Decode(bytes.NewReader(dst))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Bounds().Dx() != tc.wantW || decoded.Bounds().Dy() != tc.wantH {
			t.Fatal("image size invalid")
		}
		if jpegEXIFOrientation(dst) != 1 {
			t.Fatal("left an EXIF rotation that Excel could apply again")
		}
	}
}
func TestPixelOrientationAllEight(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetNRGBA(x, y, color.NRGBA{uint8(x + 10*y), 0, 0, 255})
		}
	}
	expect := map[int][]uint8{
		1: {0, 1, 2, 10, 11, 12}, 2: {2, 1, 0, 12, 11, 10}, 3: {12, 11, 10, 2, 1, 0}, 4: {10, 11, 12, 0, 1, 2},
		5: {0, 10, 1, 11, 2, 12}, 6: {10, 0, 11, 1, 12, 2}, 7: {12, 2, 11, 1, 10, 0}, 8: {2, 12, 1, 11, 0, 10},
	}
	for mode, want := range expect {
		got := orientPixels(src, mode)
		n := 0
		for y := 0; y < got.Bounds().Dy(); y++ {
			for x := 0; x < got.Bounds().Dx(); x++ {
				if got.NRGBAAt(x, y).R != want[n] {
					t.Fatalf("orientation %d, pixel %d: got %d expected %d", mode, n, got.NRGBAAt(x, y).R, want[n])
				}
				n++
			}
		}
	}
}

