package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func sampleJpeg(orientation int) []byte {
	im := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			im.Set(x, y, color.RGBA{uint8(25 * x), uint8(90 * y), 120, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, im, &jpeg.Options{Quality: 95})
	buf := b.Bytes()
	// Little endian EXIF IFD0 with Orientation tag; insert APP1 after JPEG SOI.
	tiff := make([]byte, 26)
	copy(tiff[:2], []byte("II"))
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)
	binary.LittleEndian.PutUint16(tiff[8:10], 1)
	binary.LittleEndian.PutUint16(tiff[10:12], 0x0112)
	binary.LittleEndian.PutUint16(tiff[12:14], 3)
	binary.LittleEndian.PutUint32(tiff[14:18], 1)
	binary.LittleEndian.PutUint16(tiff[18:20], uint16(orientation))
	data := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:4], uint16(len(data)+2))
	seg = append(seg, data...)
	out := append([]byte{}, buf[:2]...)
	out = append(out, seg...)
	out = append(out, buf[2:]...)
	return out
}
func TestWpsImageMapDoesNotUseRotation(t *testing.T) {
	xmls := `<etc:cellImages xmlns:etc="http://www.wps.cn/officeDocument/2017/etCustomData" xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><etc:cellImage><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="1" name="ID_alpha"/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rId1"/></xdr:blipFill><xdr:spPr><a:xfrm rot="5400000" flipH="1"/></xdr:spPr></xdr:pic></etc:cellImage></etc:cellImages>`
	rels := []relationship{{Id: "rId1", Target: "media/p1.png"}}
	m, e := wpsImages([]byte(xmls), rels)
	if e != nil {
		t.Fatal(e)
	}
	if p := m["ID_alpha"]; p.media != "xl/media/p1.png" {
		t.Fatalf("wrong WPS image: %+v", p)
	}
}

func TestAnchorAspectAndNoLines(t *testing.T) {
	m := metrics{defaultCol: 60, defaultRow: 48, rows: map[int]float64{}, cols: []colRange{{min: 2, max: 2, w: 500}}}
	for _, sz := range []dim{{4032, 3024}, {3024, 4032}, {400, 3}, {3, 400}} {
		a := putAnchor(placement{col: 2, row: 3, sz: sz}, m, "rId1", 1)
		if !strings.Contains(a, "<xdr:twoCellAnchor editAs=\"oneCell\">") {
			t.Fatal("missing twoCellAnchor")
		}
		if !strings.Contains(a, "<a:xfrm><a:off x=") {
			t.Fatal("missing shape transform")
		}
		ext := regexp.MustCompile(`<a:ext cx="(\d+)" cy="(\d+)"/>`).FindStringSubmatch(a)
		if len(ext) != 3 {
			t.Fatal("missing extent")
		}
		cx, _ := strconv.ParseFloat(ext[1], 64)
		cy, _ := strconv.ParseFloat(ext[2], 64)
		if cx > 496*9525 || cy > 44*9525 {
			t.Fatalf("overflow %v x %v", cx, cy)
		}
		if cx <= 0 || cy <= 0 {
			t.Fatalf("zero anchor %v x %v", cx, cy)
		}
	}
}
func TestGrowRows(t *testing.T) {
	s := []byte(`<worksheet><sheetFormatPr defaultRowHeight="15"/><sheetData><row r="3" ht="30"><c r="C3"><f>DISPIMG()</f></c></row></sheetData></worksheet>`)
	expanded := growImageRows(s, []placement{{col: 2, row: 2, sz: dim{600, 400}}}, 120)
	if !strings.Contains(string(expanded), `ht="90.00"`) || !strings.Contains(string(expanded), `customHeight="1"`) {
		t.Errorf("row not expanded: %s", expanded)
	}
}
func TestRoundTripSample(t *testing.T) {
	sample := "/mnt/data/wpsgo/sample_WPS.xlsx"
	if _, err := os.Stat(sample); err != nil {
		t.Skip("fixture unavailable")
	}
	tmp := t.TempDir()
	in := filepath.Join(tmp, "sample.xlsx")
	orig, e := os.ReadFile(sample)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(in, orig, 0600); e != nil {
		t.Fatal(e)
	}
	o := convertWorkbook(in)
	if o.err != nil || o.converted != 4 {
		t.Fatalf("conversion %+v", o)
	}
	zr, e := zip.OpenReader(o.output)
	if e != nil {
		t.Fatal(e)
	}
	defer zr.Close()
	count := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/drawings/drawing") && strings.HasSuffix(f.Name, ".xml") {
			rd, _ := f.Open()
			xmlBytes, _ := io.ReadAll(rd)
			rd.Close()
			count += strings.Count(string(xmlBytes), "name=\"WPS转换图片_")
		}
	}
	if count != 4 {
		t.Errorf("found %d anchors, expected 4", count)
	}
}

func TestFullConversionImageDataUnmodified(t *testing.T) {
	sample := "/mnt/data/wpsgo/sample_WPS.xlsx"
	source, err := zip.OpenReader(sample)
	if err != nil {
		t.Skip("fixture unavailable")
	}
	defer source.Close()
	dstDir := t.TempDir()
	srcFile := filepath.Join(dstDir, "phone_photos.xlsx")
	dst, e := os.Create(srcFile)
	if e != nil {
		t.Fatal(e)
	}
	zw := zip.NewWriter(dst)
	originalPhoto := sampleJpeg(6)
	for _, f := range source.File {
		header := f.FileHeader
		w, e := zw.CreateHeader(&header)
		if e != nil {
			t.Fatal(e)
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if f.Name == "xl/_rels/cellimages.xml.rels" {
			data = []byte(strings.ReplaceAll(string(data), "media/p1.png", "media/p1.jpg"))
		}
		if f.Name == "xl/cellimages.xml" {
			data = []byte(strings.Replace(string(data), "</xdr:blipFill></xdr:pic>", `</xdr:blipFill><xdr:spPr><a:xfrm rot="5400000" flipH="1"/></xdr:spPr></xdr:pic>`, 1))
		}
		if f.Name == "xl/media/p1.png" {
			data = []byte{}
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	w, _ := zw.Create("xl/media/p1.jpg")
	w.Write(originalPhoto)
	if e = zw.Close(); e != nil {
		t.Fatal(e)
	}
	dst.Close()
	o := convertWorkbookWithOptions(srcFile, convertOptions{enlargeImageRows: true})
	if o.err != nil {
		t.Fatalf("conversion failed: %+v", o)
	}
	if o.converted != 4 {
		t.Fatalf("expected 4 restored pictures: %+v", o)
	}
	zr, e := zip.OpenReader(o.output)
	if e != nil {
		t.Fatal(e)
	}
	defer zr.Close()
	originalPreserved := false
	drawings := 0
	otherImagePreserved := false
	for _, f := range zr.File {
		if strings.Contains(f.Name, "wps_fixed_") {
			t.Fatalf("new rotated/reencoded image generated: %s", f.Name)
		}
		if f.Name == "xl/media/p1.jpg" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			if !bytes.Equal(b, originalPhoto) {
				t.Fatal("original JPEG bytes / EXIF changed")
			}
			originalPreserved = true
		}
		if strings.HasSuffix(f.Name, "drawing1.xml") || strings.HasSuffix(f.Name, "drawing2.xml") {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			drawings += strings.Count(string(b), "name=\"WPS转换图片_")
			if strings.Contains(string(b), "已有图片") {
				otherImagePreserved = true
			}
			if strings.Contains(string(b), `rot="5400000"`) {
				t.Error("unexpected rotation in drawing")
			}
		}
		if strings.HasSuffix(f.Name, "sheet1.xml") && strings.Contains(f.Name, "worksheets") {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			if strings.Contains(string(b), "DISPIMG") {
				t.Error("old formula remains")
			}
			if !strings.Contains(string(b), "测试文字") || !strings.Contains(string(b), "<v>123</v>") {
				t.Error("other cell data damaged")
			}
		}
	}
	if !originalPreserved || drawings != 4 || !otherImagePreserved {
		t.Errorf("original photo: %t, drawings: %d, existing pic: %t", originalPreserved, drawings, otherImagePreserved)
	}
}

func TestPreserveExplicitRowHeights(t *testing.T) {
	input := []byte(`<worksheet><sheetData>` +
		`<row r="2" ht="92.85"><c r="C2"/></row>` +
		`<row r="3" ht="200.45" customHeight="0"><c r="C3"/></row>` +
		`<row r="4" ht="78.2" customHeight="1"/>` +
		`<row r="5" ht="84.25" customHeight="false"/>` +
		`<row r="6"><c r="C6"/></row>` +
		`</sheetData></worksheet>`)
	output := string(preserveExplicitRowHeights(input))
	rows := regexp.MustCompile(`<row\b[^>]*>`).FindAllString(output, -1)
	expected := []string{`ht="92.85" customHeight="1"`, `ht="200.45" customHeight="1"`, `ht="78.2" customHeight="1"`, `ht="84.25" customHeight="1"`}
	if len(rows) != 5 {
		t.Fatalf("wrong rows %v", rows)
	}
	for i, fragment := range expected {
		if !strings.Contains(rows[i], fragment) {
			t.Fatalf("row %d missing %s: %s", i+2, fragment, rows[i])
		}
	}
	if strings.Contains(rows[4], "customHeight") {
		t.Fatalf("untouched row changed: %s", rows[4])
	}
}

func TestRealWpsHeightsSurviveExcelAutofit(t *testing.T) {
	sample := "/mnt/data/清单缺失分析表-wps.xlsx"
	if _, e := os.Stat(sample); e != nil {
		t.Skip("sample not present")
	}
	in := filepath.Join(t.TempDir(), "wps.xlsx")
	data, _ := os.ReadFile(sample)
	if e := os.WriteFile(in, data, 0600); e != nil {
		t.Fatal(e)
	}
	o := convertWorkbook(in)
	if o.err != nil || o.converted != 9 || o.unmatched != 0 {
		t.Fatalf("conversion failed: %+v", o)
	}
	zr, e := zip.OpenReader(o.output)
	if e != nil {
		t.Fatal(e)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet5.xml" {
			continue
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		sheet, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		for _, idx := range []string{"2", "3", "4", "5", "6", "11", "12", "13", "14"} {
			rr := regexp.MustCompile(`<row\b[^>]*\br="` + idx + `"[^>]*>`).FindString(string(sheet))
			if rr == "" || attr(rr, "customHeight") != "1" {
				t.Fatalf("explicit height lost in row %s: %s", idx, rr)
			}
		}
		return
	}
	t.Fatal("sheet5 missing")
}

