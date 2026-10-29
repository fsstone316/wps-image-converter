package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

// jpegEXIFOrientation reads the EXIF orientation (TIFF IFD0 tag 0x0112) from
// the primary JPEG image. It does not depend on Windows' image decoders and
// supports ordinary JPEG and JPEG/MPO files used by WPS.
func jpegEXIFOrientation(buf []byte) int {
	if len(buf) < 4 || buf[0] != 0xff || buf[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(buf); {
		if buf[pos] != 0xff {
			break
		}
		marker := buf[pos+1]
		if marker == 0xd9 || marker == 0xda {
			break
		}
		if marker == 0x00 || marker == 0xff || marker == 0xd8 || (marker >= 0xd0 && marker <= 0xd7) {
			pos += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(buf[pos+2 : pos+4]))
		if n < 2 || pos+2+n > len(buf) {
			break
		}
		data := buf[pos+4 : pos+2+n]
		if marker == 0xe1 && len(data) >= 14 && bytes.Equal(data[:6], []byte("Exif\x00\x00")) {
			tiff := data[6:]
			var order binary.ByteOrder
			switch string(tiff[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				break
			}
			if order != nil && order.Uint16(tiff[2:4]) == 42 {
				off := uint64(order.Uint32(tiff[4:8]))
				if off+2 <= uint64(len(tiff)) {
					start := int(off)
					num := int(order.Uint16(tiff[start : start+2]))
					for i := 0; i < num; i++ {
						p := start + 2 + i*12
						if p+12 > len(tiff) {
							break
						}
						if order.Uint16(tiff[p:p+2]) != 0x0112 {
							continue
						}
						if order.Uint16(tiff[p+2:p+4]) != 3 || order.Uint32(tiff[p+4:p+8]) != 1 {
							continue
						}
						v := int(order.Uint16(tiff[p+8 : p+10]))
						if v >= 1 && v <= 8 {
							return v
						}
					}
				}
			}
		}
		pos += 2 + n
	}
	return 1
}

// orientPixels applies a single transformation and returns fresh pixels with
// no EXIF tag and no external drawing rotation. This prevents Excel from
// applying a second rotation after the picture has been fitted to a cell.
func orientPixels(src image.Image, orient int) *image.NRGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if orient >= 5 && orient <= 8 {
		dw, dh = sh, sw
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch orient {
			case 2:
				sx = sw - 1 - x
			case 3:
				sx, sy = sw-1-x, sh-1-y
			case 4:
				sy = sh - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, sh-1-x
			case 7:
				sx, sy = sw-1-y, sh-1-x
			case 8:
				sx, sy = sw-1-y, x
			}
			dst.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

func normalizeWpsBytes(data []byte, extension string, exif int, wpsRotation int64, flipH, flipV bool) ([]byte, string, dim, bool, error) {
	orientation := exif
	// WPS writes an xfrm 90° rotation for both ordinary JPEGs and portrait
	// JPEGs that already carry EXIF=6. For the latter, applying both would
	// rotate twice. Match the actual WPS appearance by prioritizing EXIF.
	if exif == 1 {
		rot := ((wpsRotation % 21600000) + 21600000) % 21600000
		switch rot {
		case 0:
		case 5400000:
			orientation = 6
		case 10800000:
			orientation = 3
		case 16200000:
			orientation = 8
		default:
			return nil, "", dim{}, false, fmt.Errorf("WPS 图片旋转角度 %d 暂不支持", wpsRotation)
		}
	}
	if orientation == 1 && !flipH && !flipV {
		return nil, "", dim{}, false, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", dim{}, false, fmt.Errorf("读取图片像素失败: %w", err)
	}
	oriented := orientPixels(img, orientation)
	if flipH {
		oriented = orientPixels(oriented, 2)
	}
	if flipV {
		oriented = orientPixels(oriented, 4)
	}
	targetExt := strings.ToLower(extension)
	var out bytes.Buffer
	if targetExt == "jpg" || targetExt == "jpeg" || targetExt == "jpe" || targetExt == "mpo" {
		// Re-encoding deliberately strips the original EXIF and MPO metadata.
		// The final displayed orientation is now baked into the actual pixels.
		if err = jpeg.Encode(&out, oriented, &jpeg.Options{Quality: 93}); err != nil {
			return nil, "", dim{}, false, err
		}
		targetExt = "jpg"
	} else {
		if err = png.Encode(&out, oriented); err != nil {
			return nil, "", dim{}, false, err
		}
		targetExt = "png"
	}
	return out.Bytes(), targetExt, dim{W: oriented.Bounds().Dx(), H: oriented.Bounds().Dy()}, true, nil
}

