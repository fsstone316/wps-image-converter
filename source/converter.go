package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	relNS           = "http://schemas.openxmlformats.org/package/2006/relationships"
	docRelNS        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	drawRelType     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing"
	imageRelType    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	drawContentType = "application/vnd.openxmlformats-officedocument.drawing+xml"
)

var (
	reCell               = regexp.MustCompile(`(?s)<(?:[A-Za-z0-9_]+:)?c\s+[^>]*[^/]>.*?</(?:[A-Za-z0-9_]+:)?c\s*>`)
	reOpenCell           = regexp.MustCompile(`(?s)^<(?:[A-Za-z0-9_]+:)?c\b[^>]*>`)
	reFormula            = regexp.MustCompile(`(?is)DISPIMG\s*\(\s*(?:"|&quot;|&#34;|&#x22;)([^"\s<>&]+)`)
	reDrawing            = regexp.MustCompile(`(?s)<(?:[A-Za-z0-9_]+:)?drawing\b[^>]*?/?>`)
	reCloseWs            = regexp.MustCompile(`</(?:[A-Za-z0-9_]+:)?worksheet\s*>`)
	reCloseDrawing       = regexp.MustCompile(`</(?:[A-Za-z0-9_]+:)?wsDr\s*>`)
	reNonSelfEnd         = regexp.MustCompile(`\s*/>$`)
	reCnvPr              = regexp.MustCompile(`<\s*(?:[A-Za-z0-9_]+:)?cNvPr\b[^>]*>`)
	reSheet              = regexp.MustCompile(`^xl/worksheets/[^/]+\.xml$`)
	reCol                = regexp.MustCompile(`<\s*(?:[A-Za-z0-9_]+:)?col\b[^>]*>`)
	reRow                = regexp.MustCompile(`<\s*(?:[A-Za-z0-9_]+:)?row\b[^>]*>`)
	reMerged             = regexp.MustCompile(`<\s*(?:[A-Za-z0-9_]+:)?mergeCell\b[^>]*>`)
	reSheetFormat        = regexp.MustCompile(`<\s*(?:[A-Za-z0-9_]+:)?sheetFormatPr\b[^>]*>`)
	reExt                = regexp.MustCompile(`<\s*(?:[A-Za-z0-9_]+:)?(?:legacyDrawing|legacyDrawingHF|picture|oleObjects|controls|webPublishItems|tableParts|extLst)\b`)
	reCloseRelationships = regexp.MustCompile(`</(?:[A-Za-z0-9_]+:)?Relationships\s*>`)
	reCloseTypes         = regexp.MustCompile(`</(?:[A-Za-z0-9_]+:)?Types\s*>`)
)

func attr(s, name string) string {
	re := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(name) + `\s*=\s*['"]([^'"]*)['"]`)
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func xmlQuote(s string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;", ">", "&gt;", "'", "&apos;").Replace(s)
}
func zipRead(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
func addBeforeClose(doc []byte, close *regexp.Regexp, addition string) ([]byte, error) {
	loc := close.FindIndex(doc)
	if loc == nil {
		return nil, errors.New("XML 中找不到结束标签")
	}
	var buf bytes.Buffer
	buf.Grow(len(doc) + len(addition))
	buf.Write(doc[:loc[0]])
	buf.WriteString(addition)
	buf.Write(doc[loc[0]:])
	return buf.Bytes(), nil
}

type relationship struct{ Id, Target, Type, TargetMode string }

func readRels(src []byte) ([]relationship, error) {
	if len(src) == 0 {
		return nil, nil
	}
	dec := xml.NewDecoder(bytes.NewReader(src))
	out := []relationship{}
	for {
		t, e := dec.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		start, ok := t.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		r := relationship{}
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "Id":
				r.Id = a.Value
			case "Target":
				r.Target = a.Value
			case "Type":
				r.Type = a.Value
			case "TargetMode":
				r.TargetMode = a.Value
			}
		}
		out = append(out, r)
	}
	return out, nil
}
func relID(rs []relationship) string {
	used := map[string]bool{}
	for _, r := range rs {
		used[r.Id] = true
	}
	for i := 1; ; i++ {
		s := fmt.Sprintf("rId%d", i)
		if !used[s] {
			return s
		}
	}
}
func addRelXML(src []byte, r relationship) ([]byte, error) {
	frag := fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, xmlQuote(r.Id), xmlQuote(r.Type), xmlQuote(r.Target))
	if len(src) == 0 {
		return []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="` + relNS + `">` + frag + `</Relationships>`), nil
	}
	return addBeforeClose(src, reCloseRelationships, frag)
}
func resolveTarget(sourcePart, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	return path.Clean(path.Join(path.Dir(sourcePart), target))
}

// WPS stores picture direction in EXIF and/or the shape's rotation.
// Those values have to be interpreted exactly once, BEFORE sizing/anchoring.
type wpsPicture struct {
	media        string
	rot          int64
	flipH, flipV bool
}

func wpsImages(data []byte, rels []relationship) (map[string]wpsPicture, error) {
	mediaRels := map[string]string{}
	for _, r := range rels {
		if r.TargetMode != "External" {
			mediaRels[r.Id] = resolveTarget("xl/cellimages.xml", r.Target)
		}
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	result := map[string]wpsPicture{}
	inside := false
	depth := 0
	id, embed := "", ""
	var rot int64
	flipH, flipV := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 WPS 图片映射失败: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "cellImage" && !inside {
				inside = true
				depth = 1
				id = ""
				embed = ""
				rot = 0
				flipH = false
				flipV = false
				continue
			}
			if !inside {
				continue
			}
			depth++
			for _, a := range el.Attr {
				if a.Name.Local == "name" && strings.HasPrefix(a.Value, "ID_") {
					id = a.Value
				}
				if a.Name.Local == "embed" {
					embed = a.Value
				}
				if el.Name.Local == "xfrm" {
					switch a.Name.Local {
					case "rot":
						rot, _ = strconv.ParseInt(a.Value, 10, 64)
					case "flipH":
						flipH = a.Value == "1" || strings.EqualFold(a.Value, "true")
					case "flipV":
						flipV = a.Value == "1" || strings.EqualFold(a.Value, "true")
					}
				}
			}
		case xml.EndElement:
			if !inside {
				continue
			}
			depth--
			if depth == 0 {
				if id != "" && embed != "" && mediaRels[embed] != "" {
					result[id] = wpsPicture{media: mediaRels[embed], rot: rot, flipH: flipH, flipV: flipV}
				}
				inside = false
			}
		}
	}
	return result, nil
}

type dim struct{ W, H int }

func imageSize(filename string, data []byte) dim {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil && cfg.Width > 0 && cfg.Height > 0 {
		return dim{cfg.Width, cfg.Height}
	}
	if strings.EqualFold(path.Ext(filename), ".bmp") && len(data) >= 26 {
		w := int(binary.LittleEndian.Uint32(data[18:22]))
		h := int(binary.LittleEndian.Uint32(data[22:26]))
		if w > 0 && h > 0 {
			return dim{w, h}
		}
	}
	return dim{400, 300}
}

// An image without direction metadata can be reused byte-for-byte. Images
// that need orientation normalization are copied into a new media part.
type preparedImage struct {
	media     string
	sz        dim
	ext       string
	corrected bool
}

func prepareWpsImage(_ string, p wpsPicture, files map[string]*zip.File, patches map[string][]byte, serial int) (preparedImage, error) {
	f, ok := files[p.media]
	if !ok {
		return preparedImage{}, fmt.Errorf("找不到图片 %s", p.media)
	}
	raw, err := zipRead(f)
	if err != nil {
		return preparedImage{}, err
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(p.media)), ".")
	orientation := jpegEXIFOrientation(raw)
	normalized, newExt, sz, changed, err := normalizeWpsBytes(raw, ext, orientation, p.rot, p.flipH, p.flipV)
	if err != nil {
		return preparedImage{}, err
	}
	if !changed {
		return preparedImage{media: p.media, sz: imageSize(p.media, raw), ext: ext}, nil
	}
	media := fmt.Sprintf("xl/media/wps_direction_fixed_%03d.%s", serial, newExt)
	for {
		if _, exists := files[media]; !exists {
			if _, patched := patches[media]; !patched {
				break
			}
		}
		serial++
		media = fmt.Sprintf("xl/media/wps_direction_fixed_%03d.%s", serial, newExt)
	}
	patches[media] = normalized
	return preparedImage{media: media, sz: sz, ext: newExt, corrected: true}, nil
}

func colRow(addr string) (int, int, bool) {
	addr = strings.TrimSpace(strings.ToUpper(strings.ReplaceAll(addr, "$", "")))
	c := 0
	i := 0
	for i < len(addr) && addr[i] >= 'A' && addr[i] <= 'Z' {
		c = c*26 + int(addr[i]-'A') + 1
		i++
	}
	if i == 0 || i >= len(addr) {
		return 0, 0, false
	}
	n, e := strconv.Atoi(addr[i:])
	if e != nil || n < 1 || c < 1 {
		return 0, 0, false
	}
	return c - 1, n - 1, true
}

type rangeRect struct{ col, row, endCol, endRow int }
type colRange struct {
	min, max int
	w        float64
}
type metrics struct {
	defaultCol, defaultRow float64
	cols                   []colRange
	rows                   map[int]float64
	merged                 []rangeRect
}

func parseFloat(s string, v float64) float64 {
	n, e := strconv.ParseFloat(s, 64)
	if e != nil {
		return v
	}
	return n
}
func columnPx(w float64) float64 {
	if w <= 0 {
		return 0
	}
	return math.Floor(w*7 + 5)
}
func metricsFromSheet(s []byte) metrics {
	m := metrics{defaultCol: 64, defaultRow: 20, rows: map[int]float64{}}
	if sf := reSheetFormat.Find(s); len(sf) > 0 {
		if v := attr(string(sf), "defaultColWidth"); v != "" {
			m.defaultCol = columnPx(parseFloat(v, 8.43))
		}
		if v := attr(string(sf), "defaultRowHeight"); v != "" {
			m.defaultRow = parseFloat(v, 15) * 4 / 3
		}
	}
	for _, p := range reCol.FindAll(s, -1) {
		k := string(p)
		mn, _ := strconv.Atoi(attr(k, "min"))
		mx, _ := strconv.Atoi(attr(k, "max"))
		w := attr(k, "width")
		if mn < 1 || mx < mn || w == "" {
			continue
		}
		val := columnPx(parseFloat(w, 8.43))
		if attr(k, "hidden") == "1" {
			val = 0
		}
		m.cols = append(m.cols, colRange{mn - 1, mx - 1, val})
	}
	for _, p := range reRow.FindAll(s, -1) {
		k := string(p)
		i, _ := strconv.Atoi(attr(k, "r"))
		if i < 1 {
			continue
		}
		h := m.defaultRow
		if v := attr(k, "ht"); v != "" {
			h = parseFloat(v, 15) * 4 / 3
		}
		if attr(k, "hidden") == "1" {
			h = 0
		}
		m.rows[i-1] = h
	}
	for _, p := range reMerged.FindAll(s, -1) {
		a := attr(string(p), "ref")
		parts := strings.Split(a, ":")
		if len(parts) != 2 {
			continue
		}
		c, r, ok := colRow(parts[0])
		c2, r2, ok2 := colRow(parts[1])
		if ok && ok2 {
			m.merged = append(m.merged, rangeRect{c, r, c2, r2})
		}
	}
	return m
}
func (m metrics) w(col int) float64 {
	for i := len(m.cols) - 1; i >= 0; i-- {
		q := m.cols[i]
		if q.min <= col && col <= q.max {
			return q.w
		}
	}
	return m.defaultCol
}
func (m metrics) h(row int) float64 {
	if v, ok := m.rows[row]; ok {
		return v
	}
	return m.defaultRow
}
func (m metrics) rect(col, row int) rangeRect {
	for _, r := range m.merged {
		if r.col <= col && col <= r.endCol && r.row <= row && row <= r.endRow {
			return r
		}
	}
	return rangeRect{col, row, col, row}
}
func (m metrics) span(x1, y1, x2, y2 int) (float64, float64) {
	w, h := 0.0, 0.0
	for i := x1; i <= x2; i++ {
		w += m.w(i)
	}
	for i := y1; i <= y2; i++ {
		h += m.h(i)
	}
	return w, h
}
func (m metrics) advanceCol(c int, delta float64) (int, int64) {
	for i := 0; i < 16385; i++ {
		n := m.w(c)
		if n <= 0 {
			c++
			continue
		}
		if delta < n-0.001 {
			return c, int64(math.Round(delta * 9525))
		}
		delta -= n
		c++
	}
	return c, 0
}
func (m metrics) advanceRow(r int, delta float64) (int, int64) {
	for i := 0; i < 1048577; i++ {
		n := m.h(r)
		if n <= 0 {
			r++
			continue
		}
		if delta < n-0.001 {
			return r, int64(math.Round(delta * 9525))
		}
		delta -= n
		r++
	}
	return r, 0
}

type placement struct {
	addr, id, media string
	col, row        int
	sz              dim
}

// putAnchor writes a fully specified, *bounded* Excel picture shape.
// Excel 2019 needs agreement between the sheet anchor and the shape xfrm;
// with only an anchor <ext>, some versions display the bitmap at a stale or
// intrinsic scale, which makes nearby images appear to overlap each other.
func putAnchor(p placement, m metrics, rid string, n int) string {
	return putAnchorWithOptions(p, m, rid, n, convertOptions{})
}
func putAnchorWithOptions(p placement, m metrics, rid string, n int, opts convertOptions) string {
	rect := m.rect(p.col, p.row)
	cw, ch := m.span(rect.col, rect.row, rect.endCol, rect.endRow)
	if cw <= 4 || ch <= 4 || p.sz.W < 1 || p.sz.H < 1 {
		return "" // refuse to add an out-of-bounds picture
	}
	margin := 2.0
	usableW, usableH := cw-2*margin, ch-2*margin
	scale := math.Min(usableW/float64(p.sz.W), usableH/float64(p.sz.H))
	if opts.uniformColumnWidth {
		percent := opts.widthPercent
		if percent < 20 || percent > 100 {
			percent = 100
		}
		wantScale := (usableW * float64(percent) / 100) / float64(p.sz.W)
		// Never overflow if the row could not be enlarged for any reason.
		scale = math.Min(wantScale, usableH/float64(p.sz.H))
	}
	dw, dh := float64(p.sz.W)*scale, float64(p.sz.H)*scale
	// Keep at least 2px of space on every edge, even after floating-point
	// rounding to EMUs. Never force a minimum width that could exceed a row.
	if dw <= 0 || dh <= 0 {
		return ""
	}
	x0 := (cw - dw) / 2
	y0 := (ch - dh) / 2
	fromCol, fromColOff := m.advanceCol(rect.col, x0)
	fromRow, fromRowOff := m.advanceRow(rect.row, y0)
	toCol, toColOff := m.advanceCol(rect.col, x0+dw)
	toRow, toRowOff := m.advanceRow(rect.row, y0+dh)
	cx, cy := int64(math.Round(dw*9525)), int64(math.Round(dh*9525))

	// a:xfrm coordinates are absolute worksheet EMUs, not offsets inside the
	// image anchor. Writing both a:xfrm and from/to stops Excel from restoring
	// the original JPEG size or stretching the rectangle beyond its cell.
	offX, offY := fromColOff, fromRowOff
	for c := 0; c < fromCol; c++ {
		offX += int64(math.Round(m.w(c) * 9525))
	}
	for r := 0; r < fromRow; r++ {
		offY += int64(math.Round(m.h(r) * 9525))
	}
	return fmt.Sprintf(`<xdr:twoCellAnchor editAs="oneCell"><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>%d</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>%d</xdr:rowOff></xdr:from><xdr:to><xdr:col>%d</xdr:col><xdr:colOff>%d</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>%d</xdr:rowOff></xdr:to><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="%d" name="WPS转换图片_%d"/><xdr:cNvPicPr><a:picLocks noChangeAspect="1"/></xdr:cNvPicPr></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:pic><xdr:clientData/></xdr:twoCellAnchor>`, fromCol, fromColOff, fromRow, fromRowOff, toCol, toColOff, toRow, toRowOff, n, n, xmlQuote(rid), offX, offY, cx, cy)
}

// WPS often writes <row ht="..."> without customHeight="1".
// Excel treats such an ht as a cached *automatic* row height, and on
// opening the workbook recalculates it using text content. DISPIMG removal
// changes that content: Excel can then shrink the row, while a picture
// anchor calculated from the original ht is rendered into the shrunken row.
// This was the root cause of the horizontal "strips" in C2/C3/C11 of the
// user's real-world sample. Lock only explicitly sized rows on worksheets
// where we converted pictures. Preserve the numeric ht exactly.
func preserveExplicitRowHeights(sheet []byte) []byte {
	return reRow.ReplaceAllFunc(sheet, func(row []byte) []byte {
		tag := string(row)
		ht := attr(tag, "ht")
		if ht == "" || parseFloat(ht, 0) <= 0 {
			return row
		}
		// XML attrs are case sensitive; customHeight is already present
		// in some Office-created workbooks, and can be explicitly false.
		re := regexp.MustCompile(`\bcustomHeight\s*=\s*['"][^'"]*['"]`)
		if re.MatchString(tag) {
			tag = re.ReplaceAllString(tag, `customHeight="1"`)
		} else if strings.HasSuffix(tag, "/>") {
			tag = strings.TrimSuffix(tag, "/>") + ` customHeight="1"/>`
		} else {
			tag = strings.TrimSuffix(tag, ">") + ` customHeight="1">`
		}
		return []byte(tag)
	})
}

// Optional enlarged-image mode: change only rows that contain pictures.
// Keeping this off preserves row heights and the original worksheet layout.
func growImageRows(sheet []byte, places []placement, minHeightPx float64) []byte {
	m := metricsFromSheet(sheet)
	desired := map[int]float64{}
	for _, p := range places {
		rect := m.rect(p.col, p.row)
		_, ch := m.span(rect.col, rect.row, rect.endCol, rect.endRow)
		if ch < minHeightPx {
			desired[rect.row] = math.Max(desired[rect.row], m.h(rect.row)+minHeightPx-ch)
		}
	}
	if len(desired) == 0 {
		return sheet
	}
	return reRow.ReplaceAllFunc(sheet, func(rowTag []byte) []byte {
		r, _ := strconv.Atoi(attr(string(rowTag), "r"))
		h, wanted := desired[r-1]
		if !wanted || h <= m.h(r-1) {
			return rowTag
		}
		pt := fmt.Sprintf("%.2f", h*0.75)
		tag := string(rowTag)
		htRegex := regexp.MustCompile(`\bht\s*=\s*['"][^'"]*['"]`)
		if htRegex.MatchString(tag) {
			tag = htRegex.ReplaceAllString(tag, `ht="`+pt+`"`)
		} else {
			tag = strings.TrimSuffix(tag, ">") + ` ht="` + pt + `">`
		}
		if attr(tag, "customHeight") == "" {
			tag = strings.TrimSuffix(tag, ">") + ` customHeight="1">`
		}
		return []byte(tag)
	})
}

// Uniform width mode enlarges only rows with pictures, so every picture can
// reach its chosen percentage of the *source column width* without clipping.
// This is opt-in; the validated v2.7 default layout stays unchanged.
func growRowsForUniformWidth(sheet []byte, places []placement, percent int) []byte {
	if percent < 20 || percent > 100 {
		percent = 100
	}
	m := metricsFromSheet(sheet)
	desired := map[int]float64{}
	for _, p := range places {
		if p.sz.W <= 0 || p.sz.H <= 0 {
			continue
		}
		rect := m.rect(p.col, p.row)
		cw, ch := m.span(rect.col, rect.row, rect.endCol, rect.endRow)
		if cw <= 4 || ch <= 0 {
			continue
		}
		tw := (cw - 4) * float64(percent) / 100
		required := tw*float64(p.sz.H)/float64(p.sz.W) + 4
		if required > ch+0.01 {
			// For merged cells, expand the bottom row only.
			row := rect.endRow
			desired[row] = math.Max(desired[row], m.h(row)+required-ch)
		}
	}
	if len(desired) == 0 {
		return sheet
	}
	htRegex := regexp.MustCompile(`\bht\s*=\s*['"][^'"]*['"]`)
	customRegex := regexp.MustCompile(`\bcustomHeight\s*=\s*['"][^'"]*['"]`)
	return reRow.ReplaceAllFunc(sheet, func(rowTag []byte) []byte {
		r, _ := strconv.Atoi(attr(string(rowTag), "r"))
		target, needed := desired[r-1]
		if !needed || target <= m.h(r-1) {
			return rowTag
		}
		tag := string(rowTag)
		pts := fmt.Sprintf("%.4f", target*0.75)
		if htRegex.MatchString(tag) {
			tag = htRegex.ReplaceAllString(tag, `ht="`+pts+`"`)
		} else {
			tag = strings.TrimSuffix(tag, ">") + ` ht="` + pts + `">`
		}
		if customRegex.MatchString(tag) {
			tag = customRegex.ReplaceAllString(tag, `customHeight="1"`)
		} else {
			tag = strings.TrimSuffix(tag, ">") + ` customHeight="1">`
		}
		return []byte(tag)
	})
}

func nextPicID(data []byte) int {
	max := 0
	for _, tag := range reCnvPr.FindAllString(string(data), -1) {
		i, _ := strconv.Atoi(attr(tag, "id"))
		if i > max {
			max = i
		}
	}
	return max + 1
}
func newDrawingID(exists map[string]*zip.File, patch map[string][]byte) string {
	for i := 1; ; i++ {
		a := fmt.Sprintf("xl/drawings/drawing%d.xml", i)
		if _, ok := exists[a]; ok {
			continue
		}
		if _, ok := patch[a]; ok {
			continue
		}
		return a
	}
}
func drawingsRelsPart(n string) string { return path.Join(path.Dir(n), "_rels", path.Base(n)+".rels") }
func sheetRelsPart(n string) string    { return path.Join(path.Dir(n), "_rels", path.Base(n)+".rels") }
func fileData(name string, original map[string]*zip.File, patch map[string][]byte) ([]byte, error) {
	if b, ok := patch[name]; ok {
		return b, nil
	}
	if f, ok := original[name]; ok {
		return zipRead(f)
	}
	return nil, nil
}
func isOfficeImage(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp":
		return true
	}
	return false
}
func removeCellFormula(cell string) string {
	start := reOpenCell.FindString(cell)
	if start == "" {
		return cell
	}
	// Preserve coordinate, styles, number formats, protection & other cell attributes.
	start = strings.TrimSuffix(start, ">") + "/>"
	// Empty cell must not retain string/error cell type.
	start = regexp.MustCompile(`\s+t\s*=\s*['"][^'"]*['"]`).ReplaceAllString(start, "")
	return start
}
func uniqueOutput(in string) string {
	base := strings.TrimSuffix(in, filepath.Ext(in))
	ext := filepath.Ext(in)
	for n := 0; ; n++ {
		out := base + "_Office兼容"
		if n > 0 {
			out += fmt.Sprintf("_%d", n+1)
		}
		out += ext
		if _, err := os.Stat(out); os.IsNotExist(err) {
			return out
		}
	}
}

type outcome struct {
	input, output                   string
	converted, unmatched, corrected int
	err                             error
}

type convertOptions struct {
	enlargeImageRows   bool
	uniformColumnWidth bool
	widthPercent       int
}

func convertWorkbook(input string) outcome {
	return convertWorkbookWithOptions(input, convertOptions{})
}
func convertWorkbookWithOptions(input string, opts convertOptions) outcome {
	result := outcome{input: input}
	ext := strings.ToLower(filepath.Ext(input))
	if ext != ".xlsx" && ext != ".xlsm" {
		result.err = errors.New("仅支持 XLSX / XLSM，不支持旧版 XLS")
		return result
	}
	zr, err := zip.OpenReader(input)
	if err != nil {
		result.err = fmt.Errorf("不能读取 Excel 压缩结构: %w", err)
		return result
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	p, ok := files["xl/cellimages.xml"]
	if !ok {
		result.err = errors.New("找不到 xl/cellimages.xml；文件可能不是 WPS 内嵌图片格式")
		return result
	}
	cellXml, err := zipRead(p)
	if err != nil {
		result.err = err
		return result
	}
	relXml, _ := fileData("xl/_rels/cellimages.xml.rels", files, nil)
	allRels, e := readRels(relXml)
	if e != nil {
		result.err = e
		return result
	}
	idToPic, e := wpsImages(cellXml, allRels)
	if e != nil {
		result.err = e
		return result
	}
	if len(idToPic) == 0 {
		result.err = errors.New("没有解析到 WPS 图片 ID 与图片文件的映射")
		return result
	}
	// WPS versions differ in how they write relative media Targets.
	// Verify against actual package entries rather than guessing a path.
	for id, picture := range idToPic {
		media := picture.media
		if _, present := files[media]; present {
			continue
		}
		name := path.Base(media)
		candidates := []string{
			strings.TrimPrefix(media, "xl/"),
			strings.TrimPrefix(media, "xl/xl/"),
			"xl/" + strings.TrimPrefix(media, "../"),
			"xl/media/" + name,
			"xl/pictures/" + name,
		}
		for _, c := range candidates {
			if _, present := files[c]; present {
				picture.media = c
				idToPic[id] = picture
				break
			}
		}
	}
	patches := map[string][]byte{}
	prepared := map[string]preparedImage{}
	serial := 1
	for id, picture := range idToPic {
		img, e := prepareWpsImage(id, picture, files, patches, serial)
		if e == nil {
			prepared[id] = img
			serial++
		}
	}

	addedDrawings := []string{}
	formats := map[string]bool{}
	sheetNames := []string{}
	for name := range files {
		if reSheet.MatchString(name) {
			sheetNames = append(sheetNames, name)
		}
	}
	sort.Strings(sheetNames)
	for _, name := range sheetNames {
		xmlData, err := zipRead(files[name])
		if err != nil {
			result.err = err
			return result
		}
		if !bytes.Contains(bytes.ToUpper(xmlData), []byte("DISPIMG")) {
			continue
		}
		places := []placement{}
		var out bytes.Buffer
		last := 0
		matchIndices := reCell.FindAllIndex(xmlData, -1)
		for _, index := range matchIndices {
			part := xmlData[index[0]:index[1]]
			s := string(part)
			m := reFormula.FindStringSubmatch(s)
			if len(m) < 2 {
				continue
			}
			open := reOpenCell.FindString(s)
			addr := attr(open, "r")
			c, r, found := colRow(addr)
			if !found {
				continue
			}
			img, found := prepared[m[1]]
			if !found || !isOfficeImage(img.media) {
				result.unmatched++
				continue
			}
			places = append(places, placement{addr: addr, id: m[1], media: img.media, col: c, row: r, sz: img.sz})
			if img.corrected {
				result.corrected++
			}
			formats[img.ext] = true
			out.Write(xmlData[last:index[0]])
			out.WriteString(removeCellFormula(s))
			last = index[1]
		}
		if len(places) == 0 {
			continue
		}
		out.Write(xmlData[last:])
		editedSheet := out.Bytes()
		if opts.uniformColumnWidth {
			editedSheet = growRowsForUniformWidth(editedSheet, places, opts.widthPercent)
		} else if opts.enlargeImageRows {
			editedSheet = growImageRows(editedSheet, places, 120)
		}
		// A drawing anchor uses the existing row height; ensure Excel does
		// not silently auto-fit that height when DISPIMG is removed.
		editedSheet = preserveExplicitRowHeights(editedSheet)
		sheetRelName := sheetRelsPart(name)
		sheetRelBytes, err := fileData(sheetRelName, files, patches)
		if err != nil {
			result.err = err
			return result
		}
		sheetRels, err := readRels(sheetRelBytes)
		if err != nil {
			result.err = err
			return result
		}
		drawName := ""
		drawTag := reDrawing.FindString(string(editedSheet))
		if drawTag != "" {
			id := attr(drawTag, "r:id")
			if id == "" {
				id = attr(drawTag, "id")
			}
			for _, r := range sheetRels {
				if r.Id == id && strings.HasSuffix(r.Type, "/drawing") {
					drawName = resolveTarget(name, r.Target)
					break
				}
			}
		}
		if drawName == "" {
			drawName = newDrawingID(files, patches)
			rid := relID(sheetRels)
			sheetRelBytes, err = addRelXML(sheetRelBytes, relationship{Id: rid, Type: drawRelType, Target: path.Join("..", "drawings", path.Base(drawName))})
			if err != nil {
				result.err = err
				return result
			}
			patches[sheetRelName] = sheetRelBytes
			// Insert drawing in schema order, before legacyDrawing/objects/extLst.
			tag := fmt.Sprintf(`<drawing xmlns:r="%s" r:id="%s"/>`, docRelNS, rid)
			pos := reExt.FindIndex(editedSheet)
			if pos == nil {
				pos = reCloseWs.FindIndex(editedSheet)
			}
			if pos == nil {
				result.err = fmt.Errorf("损坏的工作表 XML: %s", name)
				return result
			}
			editedSheet = append(append(append([]byte{}, editedSheet[:pos[0]]...), []byte(tag)...), editedSheet[pos[0]:]...)
			addedDrawings = append(addedDrawings, drawName)
		}
		existingDrawing, err := fileData(drawName, files, patches)
		if err != nil {
			result.err = err
			return result
		}
		if len(existingDrawing) == 0 {
			existingDrawing = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="` + docRelNS + `"></xdr:wsDr>`)
		}
		relName := drawingsRelsPart(drawName)
		drawRelBytes, err := fileData(relName, files, patches)
		if err != nil {
			result.err = err
			return result
		}
		drawRels, err := readRels(drawRelBytes)
		if err != nil {
			result.err = err
			return result
		}
		mets := metricsFromSheet(editedSheet)
		nextID := nextPicID(existingDrawing)
		var anchors strings.Builder
		for _, pl := range places {
			rid := relID(drawRels)
			rr := relationship{Id: rid, Type: imageRelType, Target: path.Join("..", "media", path.Base(pl.media))}
			// For nonstandard image paths still point exactly to original file.
			rr.Target = path.Join("..", strings.TrimPrefix(pl.media, "xl/"))
			drawRelBytes, err = addRelXML(drawRelBytes, rr)
			if err != nil {
				result.err = err
				return result
			}
			drawRels = append(drawRels, rr)
			anchor := putAnchorWithOptions(pl, mets, rid, nextID, opts)
			if anchor == "" {
				result.err = fmt.Errorf("图片 %s 的单元格太小或无法计算尺寸", pl.addr)
				return result
			}
			anchors.WriteString(anchor)
			nextID++
		}
		existingDrawing, err = addBeforeClose(existingDrawing, reCloseDrawing, anchors.String())
		if err != nil {
			result.err = fmt.Errorf("绘图 XML 更新失败: %w", err)
			return result
		}
		patches[drawName] = existingDrawing
		patches[relName] = drawRelBytes
		patches[name] = editedSheet
		result.converted += len(places)
	}
	if result.converted == 0 {
		result.err = fmt.Errorf("未转换任何图片（已识别映射 %d 项、无法匹配 %d 张）；请检查图片是否使用 DISPIMG", len(idToPic), result.unmatched)
		return result
	}
	ct, err := fileData("[Content_Types].xml", files, patches)
	if err != nil {
		result.err = err
		return result
	}
	if len(ct) == 0 {
		result.err = errors.New("Excel 缺少 [Content_Types].xml")
		return result
	}
	newContent := ""
	for _, draw := range addedDrawings {
		newContent += fmt.Sprintf(`<Override PartName="/%s" ContentType="%s"/>`, xmlQuote(draw), drawContentType)
	}
	extTypes := map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "bmp": "image/bmp"}
	for ext := range formats {
		if !bytes.Contains(bytes.ToLower(ct), []byte(`extension="`+ext+`"`)) {
			newContent += fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/>`, ext, extTypes[ext])
		}
	}
	if newContent != "" {
		ct, err = addBeforeClose(ct, reCloseTypes, newContent)
		if err != nil {
			result.err = err
			return result
		}
		patches["[Content_Types].xml"] = ct
	}
	result.output = uniqueOutput(input)
	tmp := result.output + ".tmp"
	wf, err := os.Create(tmp)
	if err != nil {
		result.err = err
		return result
	}
	zw := zip.NewWriter(wf)
	for _, file := range zr.File {
		if val, changed := patches[file.Name]; changed {
			hdr := file.FileHeader
			w, e := zw.CreateHeader(&hdr)
			if e != nil {
				err = e
				break
			}
			_, e = w.Write(val)
			if e != nil {
				err = e
				break
			}
		} else {
			// Keep compressed bytes of all original workbook parts untouched.
			if e := zw.Copy(file); e != nil {
				err = e
				break
			}
		}
	}

	if err == nil {
		newPaths := make([]string, 0)
		for n := range patches {
			if _, ok := files[n]; !ok {
				newPaths = append(newPaths, n)
			}
		}
		sort.Strings(newPaths)
		for _, n := range newPaths {
			w, e := zw.Create(n)
			if e != nil {
				err = e
				break
			}
			_, e = w.Write(patches[n])
			if e != nil {
				err = e
				break
			}
		}
	}
	e = zw.Close()
	if err == nil {
		err = e
	}
	e = wf.Close()
	if err == nil {
		err = e
	}
	if err != nil {
		os.Remove(tmp)
		result.err = err
		result.output = ""
		return result
	}
	if err = os.Rename(tmp, result.output); err != nil {
		os.Remove(tmp)
		result.err = err
		result.output = ""
		return result
	}
	return result
}

