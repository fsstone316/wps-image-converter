package main

import (
	"archive/zip"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestUniformWidthGrowsRowsAndPreservesAspect(t *testing.T) {
	src := []byte(`<worksheet><sheetFormatPr defaultRowHeight="15"/><cols><col min="3" max="3" width="70"/></cols><sheetData><row r="2" ht="25"><c r="C2"><f>DISPIMG()</f></c></row><row r="3" ht="200"><c r="C3"><f>DISPIMG()</f></c></row></sheetData></worksheet>`)
	places := []placement{{col: 2, row: 1, sz: dim{400, 100}}, {col: 2, row: 2, sz: dim{100, 400}}}
	after := growRowsForUniformWidth(src, places, 100)
	m := metricsFromSheet(after)
	if m.h(1) <= 25*4/3 || m.h(2) <= 200*4/3 {
		t.Fatalf("row heights not expanded: %s", after)
	}
	for _, p := range places {
		anchor := putAnchorWithOptions(p, m, "rId1", 4, convertOptions{uniformColumnWidth: true, widthPercent: 100})
		re := regexp.MustCompile(`<a:ext cx="([0-9]+)" cy="([0-9]+)"/>`)
		nums := re.FindStringSubmatch(anchor)
		if len(nums) != 3 {
			t.Fatalf("missing ext: %s", anchor)
		}
		x, _ := strconv.ParseFloat(nums[1], 64)
		y, _ := strconv.ParseFloat(nums[2], 64)
		width, _ := m.span(2, p.row, 2, p.row)
		target := width - 4
		if math.Abs(x/9525-target) > 0.25 {
			t.Fatalf("wrong uniform width %.2f != %.2f", x/9525, target)
		}
		if math.Abs((x/y)-float64(p.sz.W)/float64(p.sz.H)) > 0.001 {
			t.Fatalf("wrong aspect ratio")
		}
	}
}

func TestRealWpsInputUniformWidth(t *testing.T) {
	input := "/mnt/data/清单缺失分析表-wps.xlsx"
	if _, err := os.Stat(input); err != nil {
		t.Skip("sample input unavailable")
	}
	d := t.TempDir()
	dest := filepath.Join(d, "original.xlsx")
	content, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dest, content, 0600); err != nil {
		t.Fatal(err)
	}
	out := convertWorkbookWithOptions(dest, convertOptions{uniformColumnWidth: true, widthPercent: 100})
	if out.err != nil || out.converted != 9 {
		t.Fatalf("real conversion %v %v", out.converted, out.err)
	}
	zr, err := zip.OpenReader(out.output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var xml []byte
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/drawings/drawing") && strings.HasSuffix(f.Name, ".xml") {
			b, e := zipRead(f)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(b), "WPS转换图片") {
				xml = b
				break
			}
		}
	}
	if len(xml) == 0 {
		t.Fatal("no drawing XML found")
	}
	re := regexp.MustCompile(`<a:ext cx="([0-9]+)" cy="([0-9]+)"/>`)
	matches := re.FindAllStringSubmatch(string(xml), -1)
	if len(matches) < 9 {
		t.Fatalf("only %d xfrm extents", len(matches))
	}
	// All 9 picture sizes should match the source column's width.
	minW, maxW := math.MaxFloat64, 0.0
	for _, m := range matches {
		width, _ := strconv.ParseFloat(m[1], 64)
		minW = math.Min(minW, width)
		maxW = math.Max(maxW, width)
	}
	if maxW-minW > 40000 {
		t.Fatalf("image widths differ: %.0f %.0f", minW, maxW)
	}
	fmt.Printf("real WPS input: images=%d width=%.1fpx output=%.1fMB\n", out.converted, minW/9525, float64(len(content))/1e6)
}

