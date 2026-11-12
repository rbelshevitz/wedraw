package visio

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/mgilbir/gowemf"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

// MasterEmbeddedArtwork composes the embedded artwork layers from a master.
// Foreign media and native Geometry are kept as separate layers and placed
// using their own world bounds. This is important for vendor masters whose
// outer frame is a native Shape around an embedded EMF illustration.
type foreignLayerSpec struct {
	relID         string
	pinX, pinY    float64
	width, height float64
	order         int
}

type resolvedForeignLayer struct {
	data []byte
	foreignLayerSpec
}

var errNativeMetafileUnavailable = errors.New("native GDI+ metafile rendering is unavailable")

var metafileResolution = 2048

// SetMetafileResolution sets the target long edge, in pixels, used when a
// Windows EMF is replayed to PNG. A larger value improves zoom quality but
// increases output size and peak memory.
func SetMetafileResolution(pixels int) {
	if pixels < 256 {
		pixels = 256
	}
	if pixels > 8192 {
		pixels = 8192
	}
	metafileResolution = pixels
}

func MasterEmbeddedArtwork(stencil *vsdx.Stencil, master *vsdx.Page) ([]byte, float64, float64, bool, error) {
	if stencil == nil || master == nil || master.XML() == nil || master.XML().Root() == nil {
		return nil, 0, 0, false, nil
	}
	var specs []foreignLayerSpec
	shapeOrder := make(map[string]int)
	nextOrder := 0
	var walk func(*etree.Element)
	walk = func(elem *etree.Element) {
		if elem.Tag == "Shape" {
			id := elem.SelectAttrValue("ID", "")
			if id != "" {
				shapeOrder[id] = nextOrder
				nextOrder++
			}
		}
		if elem.Tag == "Shape" && strings.EqualFold(elem.SelectAttrValue("Type", ""), "Foreign") {
			if fd := elem.SelectElement("ForeignData"); fd != nil {
				if rel := fd.SelectElement("Rel"); rel != nil {
					if id := rel.SelectAttrValue("id", ""); id != "" {
						specs = append(specs, foreignLayerSpec{
							relID: id,
							pinX:  cellNumber(elem, "PinX"), pinY: cellNumber(elem, "PinY"),
							width: cellNumber(elem, "Width"), height: cellNumber(elem, "Height"),
							order: shapeOrder[elem.SelectAttrValue("ID", "")],
						})
					}
				}
			}
		}
		for _, child := range elem.ChildElements() {
			walk(child)
		}
	}
	walk(master.XML().Root())
	if len(specs) == 0 {
		return nil, 0, 0, false, nil
	}

	relsPath := filepath.ToSlash(filepath.Join("visio", "masters", "_rels", "master"+master.PageID()+".xml.rels"))
	relsData, ok := stencil.ZipFileContents[relsPath]
	if !ok {
		return nil, 0, 0, false, nil
	}
	relsDoc := etree.NewDocument()
	if err := relsDoc.ReadFromBytes(relsData); err != nil {
		return nil, 0, 0, false, err
	}
	relTargets := make(map[string]string)
	for _, rel := range relsDoc.Root().SelectElements("Relationship") {
		if id := rel.SelectAttrValue("Id", ""); id != "" {
			relTargets[id] = rel.SelectAttrValue("Target", "")
		}
	}
	var layers []resolvedForeignLayer
	for _, spec := range specs {
		target := relTargets[spec.relID]
		if target == "" {
			continue
		}
		mediaPath := filepath.ToSlash(filepath.Join("visio", "media", filepath.Base(target)))
		if data, ok := stencil.ZipFileContents[mediaPath]; ok && strings.EqualFold(filepath.Ext(mediaPath), ".svg") {
			layers = append(layers, resolvedForeignLayer{data: data, foreignLayerSpec: spec})
		}
	}
	if len(layers) == 0 {
		layers = renderTreeForeignLayers(master)
		if len(layers) == 0 {
			return nil, 0, 0, false, nil
		}
	} else {
		// Relationship-backed Foreign layers were initially read from their
		// raw PinX/PinY cells. Native siblings use resolved world bounds, so
		// normalize both kinds before composing them. Otherwise a Visio
		// group's local Y axis can leave a visible gap or shift the image.
		worldBounds := foreignWorldBounds(master)
		for i := range layers {
			if bounds, ok := worldBounds[layers[i].relID]; ok {
				bounds.order = layers[i].order
				layers[i].foreignLayerSpec = bounds
			}
		}
	}
	// Many HPE masters put the chassis/background in a native Shape before
	// the Foreign image. Do not discard it: it carries the visible frame,
	// fills, and borders that are absent from the EMF itself.
	if nativeLayers := renderTreeNativeLayers(master, shapeOrder); len(nativeLayers) > 0 {
		// Keep the native background/frame. Once Foreign and native layers use
		// the same world bounds, a neutral rectangle is a real underlay rather
		// than the misplaced top block produced by the old Y mapping.
		layers = append(nativeLayers, layers...)
	}
	sort.SliceStable(layers, func(i, j int) bool { return layers[i].order < layers[j].order })
	artwork, width, height := composeSVGLayers(layers)
	return artwork, width, height, true, nil
}

func foreignWorldBounds(master *vsdx.Page) map[string]foreignLayerSpec {
	result := make(map[string]foreignLayerSpec)
	children := master.ChildShapes()
	if len(children) == 0 {
		return result
	}
	transforms := vsdx.ComputeGroupTransforms(children[0])
	var walk func(*vsdx.Shape)
	walk = func(shape *vsdx.Shape) {
		if shape == nil {
			return
		}
		if strings.EqualFold(shape.ShapeType, "Foreign") {
			if fd := shape.XML().SelectElement("ForeignData"); fd != nil {
				if rel := fd.SelectElement("Rel"); rel != nil {
					if relID := rel.SelectAttrValue("id", ""); relID != "" {
						if transform := transforms[shape.ID]; transform != nil {
							bounds := transform.WorldBounds
							if bounds.Width > 0 && bounds.Height > 0 {
								result[relID] = foreignLayerSpec{
									relID: relID,
									pinX:  bounds.X + bounds.Width/2, pinY: bounds.Y + bounds.Height/2,
									width: bounds.Width, height: bounds.Height,
									order: shapeOrderFor(master, shape.ID),
								}
							}
						}
					}
				}
			}
		}
		for _, child := range shape.ChildShapes() {
			walk(child)
		}
	}
	walk(children[0])
	return result
}

// renderTreeForeignLayers is a fallback for masters whose relationships were
// repaired in memory but are not visible through the package's cached media
// lookup. Render each Foreign node independently and place it using the
// accumulated group transform instead of falling back to a degenerate root
// Group (many Lenovo masters intentionally have an almost-zero root height).
func renderTreeForeignLayers(master *vsdx.Page) []resolvedForeignLayer {
	children := master.ChildShapes()
	if len(children) == 0 {
		return nil
	}
	root := children[0]
	transforms := vsdx.ComputeGroupTransforms(root)
	var layers []resolvedForeignLayer
	var walk func(*vsdx.Shape)
	walk = func(shape *vsdx.Shape) {
		if shape == nil {
			return
		}
		if strings.EqualFold(shape.ShapeType, "Foreign") {
			if rendered, err := vsdx.ShapeToSVG(shape, vsdx.WithPrecision(2)); err == nil && rendered != nil && len(rendered.SVG) > 0 {
				if transform := transforms[shape.ID]; transform != nil {
					bounds := transform.WorldBounds
					if bounds.Width > 0 && bounds.Height > 0 {
						layers = append(layers, resolvedForeignLayer{
							data: rendered.SVG,
							foreignLayerSpec: foreignLayerSpec{
								pinX: bounds.X + bounds.Width/2, pinY: bounds.Y + bounds.Height/2,
								width: bounds.Width, height: bounds.Height,
								order: shapeOrderFor(master, shape.ID),
							},
						})
					}
				}
			}
		}
		for _, child := range shape.ChildShapes() {
			walk(child)
		}
	}
	walk(root)
	return layers
}

// renderTreeNativeLayers renders visible native shapes that share the master
// tree with Foreign artwork. Each shape keeps its own world bounds rather than
// being stretched over the Foreign canvas; this preserves HPE outlines and
// colored panels without reintroducing the old full-master rectangle overlay.
func renderTreeNativeLayers(master *vsdx.Page, shapeOrder map[string]int) []resolvedForeignLayer {
	children := master.ChildShapes()
	if len(children) == 0 {
		return nil
	}
	root := children[0]
	transforms := vsdx.ComputeGroupTransforms(root)
	var layers []resolvedForeignLayer
	var walk func(*vsdx.Shape, bool)
	walk = func(shape *vsdx.Shape, isRoot bool) {
		if shape == nil {
			return
		}
		if !isRoot && !strings.EqualFold(shape.ShapeType, "Foreign") && shape.ShapeType != "Group" {
			if rendered, err := vsdx.ShapeToSVG(shape, vsdx.WithPrecision(2)); err == nil && rendered != nil && len(rendered.SVG) > 0 {
				if transform := transforms[shape.ID]; transform != nil {
					bounds := transform.WorldBounds
					if bounds.Width > 0 && bounds.Height > 0 {
						layers = append(layers, resolvedForeignLayer{
							data: rendered.SVG,
							foreignLayerSpec: foreignLayerSpec{
								pinX: bounds.X + bounds.Width/2, pinY: bounds.Y + bounds.Height/2,
								width: bounds.Width, height: bounds.Height,
								order: shapeOrder[shape.ID],
							},
						})
					}
				}
			}
		}
		for _, child := range shape.ChildShapes() {
			walk(child, false)
		}
	}
	walk(root, true)
	return layers
}

func shapeOrderFor(master *vsdx.Page, id string) int {
	if master == nil || master.XML() == nil || master.XML().Root() == nil {
		return 0
	}
	order := 0
	found := -1
	var walk func(*etree.Element)
	walk = func(elem *etree.Element) {
		if found >= 0 {
			return
		}
		if elem.Tag == "Shape" {
			if elem.SelectAttrValue("ID", "") == id {
				found = order
				return
			}
			order++
		}
		for _, child := range elem.ChildElements() {
			walk(child)
		}
	}
	walk(master.XML().Root())
	if found >= 0 {
		return found
	}
	return 0
}

func cellNumber(shape *etree.Element, name string) float64 {
	if shape == nil {
		return 0
	}
	for _, cell := range shape.SelectElements("Cell") {
		if cell.SelectAttrValue("N", "") == name {
			value, _ := strconv.ParseFloat(cell.SelectAttrValue("V", ""), 64)
			return value
		}
	}
	return 0
}

func shapeCellNumber(shape *vsdx.Shape, name string) float64 {
	if shape == nil {
		return 0
	}
	value, _ := strconv.ParseFloat(shape.CellValue(name), 64)
	return value
}

func stripSVGText(svg []byte) []byte {
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil || document.Root() == nil {
		return svg
	}
	for _, text := range document.Root().FindElements(".//text") {
		if parent := text.Parent(); parent != nil {
			parent.RemoveChild(text)
		}
	}
	updated, err := document.WriteToBytes()
	if err != nil {
		return svg
	}
	return updated
}

func composeSVGLayers(layers []resolvedForeignLayer) ([]byte, float64, float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, layer := range layers {
		minX = math.Min(minX, layer.pinX-layer.width/2)
		minY = math.Min(minY, layer.pinY-layer.height/2)
		maxX = math.Max(maxX, layer.pinX+layer.width/2)
		maxY = math.Max(maxY, layer.pinY+layer.height/2)
	}
	canvasWidth, canvasHeight := maxX-minX, maxY-minY
	if canvasWidth <= 0 || canvasHeight <= 0 {
		return layers[0].data, layers[0].width, layers[0].height
	}
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	b.WriteString(formatNumber(canvasWidth))
	b.WriteString(" ")
	b.WriteString(formatNumber(canvasHeight))
	b.WriteString(`" width="`)
	b.WriteString(formatNumber(canvasWidth))
	b.WriteString(`" height="`)
	b.WriteString(formatNumber(canvasHeight))
	b.WriteString(`">`)
	for _, layer := range layers {
		s := string(layer.data)
		start := strings.IndexByte(s, '>')
		end := strings.LastIndex(s, "</svg>")
		if start >= 0 && end > start {
			opening := s[:start+1]
			if viewBox, ok := svgAttribute(opening, "viewBox"); ok {
				left := layer.pinX - layer.width/2 - minX
				top := maxY - (layer.pinY + layer.height/2)
				b.WriteString(`<svg x="`)
				b.WriteString(formatNumber(left))
				b.WriteString(`" y="`)
				b.WriteString(formatNumber(top))
				b.WriteString(`" width="`)
				b.WriteString(formatNumber(layer.width))
				b.WriteString(`" height="`)
				b.WriteString(formatNumber(layer.height))
				b.WriteString(`" viewBox="`)
				b.WriteString(viewBox)
				b.WriteString(`" preserveAspectRatio="xMidYMid meet">`)
				b.WriteString(s[start+1 : end])
				b.WriteString(`</svg>`)
			} else {
				b.WriteString(s[start+1 : end])
			}
		}
	}
	b.WriteString(`</svg>`)
	return []byte(b.String()), canvasWidth, canvasHeight
}

// AppendSVGOverlay adds a native geometry SVG inside an existing composed
// artwork SVG. Only the small vector overlay is copied; raster layers are not
// base64-wrapped a second time.
func AppendSVGOverlay(base, overlay []byte) []byte {
	baseText, overlayText := string(base), string(overlay)
	baseEnd := strings.LastIndex(baseText, "</svg>")
	overlayStart := strings.IndexByte(overlayText, '>')
	overlayEnd := strings.LastIndex(overlayText, "</svg>")
	if baseEnd < 0 || overlayStart < 0 || overlayEnd <= overlayStart {
		return base
	}
	var b strings.Builder
	b.Grow(len(baseText) + len(overlayText))
	b.WriteString(baseText[:baseEnd])
	b.WriteString(`<svg x="0" y="0" width="100%" height="100%"`)
	if viewBox, ok := svgAttribute(overlayText[:overlayStart+1], "viewBox"); ok {
		b.WriteString(` viewBox="`)
		b.WriteString(viewBox)
		b.WriteString(`" preserveAspectRatio="none"`)
	}
	b.WriteByte('>')
	b.WriteString(overlayText[overlayStart+1 : overlayEnd])
	b.WriteString(`</svg>`)
	b.WriteString(baseText[baseEnd:])
	return []byte(b.String())
}

func svgAttribute(opening, name string) (string, bool) {
	marker := name + `="`
	start := strings.Index(opening, marker)
	if start < 0 {
		return "", false
	}
	start += len(marker)
	end := strings.IndexByte(opening[start:], '"')
	if end < 0 {
		return "", false
	}
	return opening[start : start+end], true
}

// ConvertEmbeddedMetafiles converts EMF/WMF media in a loaded VSSX to SVG.
// The relationships are changed in memory, so vsdx-go can consume the result
// through its normal ForeignImageData/DataURI path without an external helper.
func ConvertEmbeddedMetafiles(stencil *vsdx.Stencil) error {
	if stencil == nil || len(stencil.ZipFileContents) == 0 {
		return nil
	}

	converted := make(map[string][]byte)
	replacements := make(map[string]string)
	originals := make([]string, 0)
	for path, data := range stencil.ZipFileContents {
		ext := strings.ToLower(path)
		if !strings.HasSuffix(ext, ".emf") && !strings.HasSuffix(ext, ".wmf") {
			continue
		}
		svg, err := metafileToSVG(data)
		if err != nil {
			return fmt.Errorf("render embedded metafile %q: %w", path, err)
		}
		svgPath := path[:len(path)-4] + ".svg"
		converted[svgPath] = svg
		replacements[path] = svgPath
		originals = append(originals, path)
	}
	for path, data := range converted {
		stencil.ZipFileContents[path] = data
	}
	for _, path := range originals {
		delete(stencil.ZipFileContents, path)
	}
	for path, data := range stencil.ZipFileContents {
		if !strings.Contains(path, "_rels/") || !bytes.Contains(data, []byte(".emf")) && !bytes.Contains(data, []byte(".wmf")) {
			continue
		}
		updated := data
		for original, replacement := range replacements {
			oldName := []byte(original[strings.LastIndex(original, "/")+1:])
			newName := []byte(replacement[strings.LastIndex(replacement, "/")+1:])
			updated = bytes.ReplaceAll(updated, oldName, newName)
		}
		stencil.ZipFileContents[path] = updated
	}
	return nil
}

type svgMetafileBackend struct {
	width, height float64
	content       strings.Builder
}

func metafileToSVG(data []byte) ([]byte, error) {
	if native, err := nativeMetafileToSVG(data); err == nil {
		return native, nil
	}
	header, err := gowemf.Walk(data, gowemf.Limits{}, nil)
	if err != nil {
		return nil, err
	}
	width, height := metafileSize(header)
	backend := &svgMetafileBackend{width: width, height: height}
	_, err = gowemf.Play(data, gowemf.PlayOptions{
		Destination:    gowemf.Box{Width: width, Height: height},
		CustomLineCaps: true,
		Unsupported:    func(gowemf.UnsupportedOperation) error { return nil },
	}, backend)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s">%s</svg>`,
		formatNumber(width), formatNumber(height), formatNumber(width), formatNumber(height), backend.content.String())), nil
}

func metafileSize(header gowemf.Header) (float64, float64) {
	if header.EMF != nil {
		w := math.Abs(float64(header.EMF.Frame.Right - header.EMF.Frame.Left))
		h := math.Abs(float64(header.EMF.Frame.Bottom - header.EMF.Frame.Top))
		if w > 0 && h > 0 {
			return w, h
		}
		w = math.Abs(float64(header.EMF.Bounds.Right - header.EMF.Bounds.Left))
		h = math.Abs(float64(header.EMF.Bounds.Bottom - header.EMF.Bounds.Top))
		if w > 0 && h > 0 {
			return w, h
		}
	}
	if header.Placeable != nil {
		w := math.Abs(float64(header.Placeable.Bounds.Right - header.Placeable.Bounds.Left))
		h := math.Abs(float64(header.Placeable.Bounds.Bottom - header.Placeable.Bounds.Top))
		if w > 0 && h > 0 {
			return w, h
		}
	}
	return 1000, 1000
}

func (b *svgMetafileBackend) FillPath(path gowemf.Path, rule gowemf.FillRule, paint gowemf.Paint, _ gowemf.Clip) error {
	d := svgPath(path)
	if d == "" {
		return nil
	}
	fill := paintColor(paint)
	f := "fill=\"" + fill + "\""
	if rule == gowemf.EvenOdd {
		f += ` fill-rule="evenodd"`
	}
	b.content.WriteString(`<path d="` + d + `" ` + f + `/>`)
	return nil
}

func (b *svgMetafileBackend) StrokePath(path gowemf.Path, stroke gowemf.Stroke, _ gowemf.Clip) error {
	d := svgPath(path)
	if d == "" {
		return nil
	}
	width := stroke.Width
	if stroke.Hairline || width <= 0 {
		width = 1
	}
	b.content.WriteString(`<path d="` + d + `" fill="none" stroke="` + paintColor(stroke.Paint) + `" stroke-width="` + formatNumber(width) + `"`)
	if stroke.Cap == gowemf.CapRound {
		b.content.WriteString(` stroke-linecap="round"`)
	} else if stroke.Cap == gowemf.CapSquare {
		b.content.WriteString(` stroke-linecap="square"`)
	}
	if stroke.Join == gowemf.JoinRound {
		b.content.WriteString(` stroke-linejoin="round"`)
	} else if stroke.Join == gowemf.JoinBevel {
		b.content.WriteString(` stroke-linejoin="bevel"`)
	}
	b.content.WriteString(`/>`)
	return nil
}

func (b *svgMetafileBackend) DrawImage(draw gowemf.ImageDraw, _ gowemf.Clip) error {
	if draw.Image == nil {
		return nil
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, draw.Image); err != nil {
		return err
	}
	min := draw.Image.Bounds().Min
	max := draw.Image.Bounds().Max
	if max.X <= min.X || max.Y <= min.Y {
		return nil
	}
	b.content.WriteString(`<image href="data:image/png;base64,` + base64.StdEncoding.EncodeToString(encoded.Bytes()) + `" x="` + formatNumber(draw.Transform.Dx) + `" y="` + formatNumber(draw.Transform.Dy) + `" width="` + formatNumber(draw.Transform.M11*float64(max.X-min.X)) + `" height="` + formatNumber(draw.Transform.M22*float64(max.Y-min.Y)) + `"/>`)
	return nil
}

// DrawRaster is intentionally conservative: when EMF uses a raster operation
// for an icon layer, preserve the source image instead of dropping the layer.
// Exact ROP composition is not representable in plain SVG without a raster
// surface, but showing the source is materially better than losing artwork.
func (b *svgMetafileBackend) DrawRaster(draw gowemf.RasterDraw, clip gowemf.Clip) error {
	if draw.Source == nil {
		return nil
	}
	return b.DrawImage(*draw.Source, clip)
}

func svgPath(path gowemf.Path) string {
	var b strings.Builder
	point := 0
	for _, verb := range path.Verbs {
		switch verb {
		case gowemf.PathMoveTo:
			if point >= len(path.Points) {
				return b.String()
			}
			p := path.Points[point]
			point++
			b.WriteString("M" + formatNumber(p.X) + " " + formatNumber(p.Y))
		case gowemf.PathLineTo:
			if point >= len(path.Points) {
				return b.String()
			}
			p := path.Points[point]
			point++
			b.WriteString("L" + formatNumber(p.X) + " " + formatNumber(p.Y))
		case gowemf.PathCubicTo:
			if point+2 >= len(path.Points) {
				return b.String()
			}
			p1, p2, p3 := path.Points[point], path.Points[point+1], path.Points[point+2]
			point += 3
			b.WriteString("C" + formatNumber(p1.X) + " " + formatNumber(p1.Y) + " " + formatNumber(p2.X) + " " + formatNumber(p2.Y) + " " + formatNumber(p3.X) + " " + formatNumber(p3.Y))
		case gowemf.PathClose:
			b.WriteByte('Z')
		}
	}
	return b.String()
}

func paintColor(paint gowemf.Paint) string {
	c := paint.Color
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%s)", c.R, c.G, c.B, formatNumber(float64(c.A)/255))
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}
