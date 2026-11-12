package library

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

var svgPathToken = regexp.MustCompile(`[AaCcHhLlMmQqSsTtVvZz]|[-+]?(?:\d*\.\d+|\d+\.?)(?:[eE][-+]?\d+)?`)

// NewRenderTreeUngroupEntry emits nested mxCell groups from vsdx-go's
// resolved render tree. The tree already contains inherited geometry and
// transforms, so this avoids reconstructing Visio placement from flattened
// SVG. Unsupported render features return an error and the caller can use the
// faithful SVG fallback.
func NewRenderTreeUngroupEntry(title string, groupWidth, groupHeight, artworkWidth, artworkHeight int, root *vsdx.RenderNode, points string) (Entry, error) {
	return newRenderTreeUngroupEntry(title, groupWidth, groupHeight, artworkWidth, artworkHeight, root, points, false)
}

// NewFlatRenderTreeUngroupEntry emits all safe native elements directly under
// the outer library group. This is the user-facing form: one Arrange > Ungroup
// exposes the paths and text instead of making users open structural groups.
func NewFlatRenderTreeUngroupEntry(title string, groupWidth, groupHeight, artworkWidth, artworkHeight int, root *vsdx.RenderNode, points string) (Entry, error) {
	return newRenderTreeUngroupEntry(title, groupWidth, groupHeight, artworkWidth, artworkHeight, root, points, true)
}

// RenderTreeIsSimple reports whether native decomposition is unlikely to
// change the artwork. A single geometry path may be exposed safely; grouped
// or multi-path artwork stays as the original SVG instead of being reduced.
func RenderTreeIsSimple(root *vsdx.RenderNode) bool {
	if root == nil || renderTreeHasUnsupportedFeature(root) {
		return false
	}
	paths := 0
	var visit func(*vsdx.RenderNode)
	visit = func(node *vsdx.RenderNode) {
		if node == nil || !node.Visible {
			return
		}
		paths += len(node.Geometry)
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(root)
	return paths == 1
}

func newRenderTreeUngroupEntry(title string, groupWidth, groupHeight, artworkWidth, artworkHeight int, root *vsdx.RenderNode, points string, flatten bool) (Entry, error) {
	if root == nil || artworkWidth < 1 || artworkHeight < 1 {
		return Entry{}, fmt.Errorf("render tree is empty")
	}
	if renderTreeHasUnsupportedFeature(root) {
		return Entry{}, fmt.Errorf("render tree contains geometry or effects that are not safe to split without visual changes")
	}
	nextID := 3
	var cells strings.Builder
	if err := appendRenderNodeCells(&cells, &nextID, root, "2", artworkWidth, artworkHeight, groupWidth, groupHeight, flatten); err != nil {
		return Entry{}, err
	}
	if cells.Len() == 0 {
		return Entry{}, fmt.Errorf("render tree contains no editable geometry")
	}
	groupStyle := "group;html=1;" + points
	model := fmt.Sprintf(
		`<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="" style="%s" vertex="1" parent="1"><mxGeometry width="%d" height="%d" as="geometry"/></mxCell>%s</root></mxGraphModel>`,
		xmlEscapeAttr(groupStyle), groupWidth, groupHeight, cells.String())
	return Entry{Title: title, Width: groupWidth, Height: groupHeight, XML: model}, nil
}

func renderTreeHasUnsupportedFeature(node *vsdx.RenderNode) bool {
	if node == nil {
		return false
	}
	if node.Image != nil || len(node.Markers) > 0 || len(node.Gradients) > 0 || len(node.Shadows) > 0 || len(node.FillPatterns) > 0 {
		return true
	}
	// Multiple paths can form one compound Visio contour with holes. Splitting
	// them into independent mxCells changes the fill rule and can make icons
	// look solid or otherwise simplified. Keep the complete SVG in that case.
	if len(node.Geometry) > 1 || node.Angle != 0 {
		return true
	}
	for _, path := range node.Geometry {
		if path == nil || path.FillOpacity > 0 && path.FillOpacity < 1 ||
			path.DashArray != "" || path.VectorEffect != "" ||
			path.MarkerStartID != "" || path.MarkerEndID != "" {
			return true
		}
	}
	if text := node.Text; text != nil {
		if len(text.Lines) > 1 || text.Transform != "" || text.WritingMode != "" ||
			(text.TextAnchor != "" && text.TextAnchor != "middle") ||
			(text.Baseline != "" && text.Baseline != "alphabetic") ||
			text.TextDecoration != "" || text.SmallCaps || text.TextPos != 0 ||
			text.BackgroundFill != "" {
			return true
		}
	}
	for _, child := range node.Children {
		if renderTreeHasUnsupportedFeature(child) {
			return true
		}
	}
	return false
}

func appendRenderNodeCells(cells *strings.Builder, nextID *int, node *vsdx.RenderNode, parent string, width, height, labelWidth, labelHeight int, flatten bool) error {
	if node == nil {
		return nil
	}
	if !node.Visible {
		return nil
	}
	currentParent := parent
	if len(node.Children) > 0 && !flatten {
		id := strconv.Itoa(*nextID)
		*nextID = *nextID + 1
		cells.WriteString(fmt.Sprintf(`<mxCell id="%s" value="" style="group;" vertex="1" parent="%s"><mxGeometry x="0" y="0" width="%d" height="%d" as="geometry"/></mxCell>`, id, parent, width, height))
		currentParent = id
	}
	// The SVG renderer draws child geometry before the parent's geometry.
	// Preserve that z-order in the mxCell stream as well.
	for _, child := range node.Children {
		if err := appendRenderNodeCells(cells, nextID, child, currentParent, width, height, labelWidth, labelHeight, flatten); err != nil {
			return err
		}
	}
	for _, path := range node.Geometry {
		if path.NoShow || strings.TrimSpace(path.D) == "" {
			continue
		}
		commands, err := svgPathToStencil(path.D, [4]float64{0, 0, float64(width), float64(height)}, width, height)
		if err != nil {
			return fmt.Errorf("convert resolved path: %w", err)
		}
		cells.WriteString(resolvedPathCell(*nextID, width, height, commands, path, currentParent))
		*nextID = *nextID + 1
	}
	if node.Text != nil && strings.TrimSpace(node.Text.Content) != "" {
		cells.WriteString(resolvedTextCell(*nextID, node.Text, labelWidth, labelHeight, currentParent))
		*nextID = *nextID + 1
	}
	return nil
}

func resolvedPathCell(id, width, height int, commands string, path *vsdx.ResolvedPath, parent string) string {
	shape := fmt.Sprintf(`<shape name="wedraw-path" w="%d" h="%d" aspect="variable" strokewidth="inherit"><foreground>`, width, height)
	if path.Fill != "none" && path.Fill != "" {
		shape += `<fillcolor color="fill"/>`
	}
	if path.Stroke != "none" && path.Stroke != "" {
		shape += `<strokecolor color="stroke"/>`
		shape += fmt.Sprintf(`<strokewidth width="%s" fixed="1"/>`, fmtNum(path.StrokeWidth))
	}
	shape += `<path>` + commands + `</path>`
	if path.Fill != "none" && path.Fill != "" && path.Stroke != "none" && path.Stroke != "" {
		shape += `<fillstroke/>`
	} else if path.Fill != "none" && path.Fill != "" {
		shape += `<fill/>`
	} else {
		shape += `<stroke/>`
	}
	shape += `</foreground></shape>`
	compressed, err := compressStencil(shape)
	if err != nil {
		return ""
	}
	style := xmlEscapeAttr(nativeColorStyle("shape=stencil("+compressed+");html=1;", path.Fill, path.Stroke))
	return fmt.Sprintf(`<mxCell id="%d" value="" style="%s" vertex="1" parent="%s"><mxGeometry x="0" y="0" width="%d" height="%d" as="geometry"/></mxCell>`, id, style, parent, width, height)
}

func resolvedTextCell(id int, text *vsdx.ResolvedText, width, height int, parent string) string {
	fontSize := int(math.Round(text.FontSize))
	if fontSize < 1 {
		fontSize = 12
	}
	fontSize = int(math.Round(float64(fontSize) * 0.72))
	if fontSize < 8 {
		fontSize = 8
	}
	if fontSize > 18 {
		fontSize = 18
	}
	labelHeight := int(math.Ceil(float64(fontSize) * 1.35))
	if labelHeight < 18 {
		labelHeight = 18
	}
	y := height - labelHeight - labelGapPixels
	if y < 0 {
		y = 0
	}
	style := "text;html=1;whiteSpace=wrap;align=center;verticalAlign=bottom;spacingBottom=1;fontColor=" + text.Fill + ";fontSize=" + strconv.Itoa(fontSize) + ";"
	if text.FontFamily != "" {
		style += "fontFamily=" + text.FontFamily + ";"
	}
	if text.FontWeight == "bold" {
		style += "fontStyle=1;"
	}
	return fmt.Sprintf(`<mxCell id="%d" value="%s" style="%s" vertex="1" parent="%s"><mxGeometry x="0" y="%d" width="%d" height="%d" as="geometry"/></mxCell>`, id, xmlEscapeAttr(text.Content), xmlEscapeAttr(style), parent, y, width, labelHeight)
}

// NewSVGUngroupEntry decomposes simple SVG path/text artwork into child cells.
// If the SVG contains unsupported elements or path commands, it falls back to
// the faithful hybrid entry instead of dropping or approximating graphics.
func NewSVGUngroupEntry(title string, width, height int, svg []byte, points string) (Entry, error) {
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil {
		return NewSVGGroupEntry(title, width, height, svg, points)
	}
	root := document.Root()
	if root == nil {
		return NewSVGGroupEntry(title, width, height, svg, points)
	}
	viewBox, ok := parseSVGViewBox(root.SelectAttrValue("viewBox", ""))
	if !ok || viewBox[2] <= 0 || viewBox[3] <= 0 {
		return NewSVGGroupEntry(title, width, height, svg, points)
	}
	texts := root.FindElements(".//text")
	paths := root.FindElements(".//path")
	if len(paths) == 0 {
		return NewSVGGroupEntry(title, width, height, svg, points)
	}
	for _, child := range root.ChildElements() {
		if child.Tag != "path" && child.Tag != "text" {
			return NewSVGGroupEntry(title, width, height, svg, points)
		}
	}

	pathCells := make([]string, 0, len(paths))
	for index, path := range paths {
		commands, err := svgPathToStencil(path.SelectAttrValue("d", ""), viewBox, width, height)
		if err != nil {
			return NewSVGGroupEntry(title, width, height, svg, points)
		}
		pathCells = append(pathCells, pathCell(index+3, width, height, commands, path))
	}
	textCells := make([]string, 0, len(texts))
	for index, text := range texts {
		value := strings.TrimSpace(svgTextContent(text))
		if value != "" {
			textCells = append(textCells, groupTextCell(len(pathCells)+index+3, value, width, height, text))
		}
	}

	groupStyle := "group;html=1;" + points
	model := fmt.Sprintf(
		`<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="" style="%s" vertex="1" parent="1"><mxGeometry width="%d" height="%d" as="geometry"/></mxCell>%s%s</root></mxGraphModel>`,
		xmlEscapeAttr(groupStyle), width, height, strings.Join(pathCells, ""), strings.Join(textCells, ""))
	return Entry{Title: title, Width: width, Height: height, XML: model}, nil
}

// SVGIsFlatPathArtwork reports whether the SVG can be represented as native
// draw.io stencil cells without hiding the artwork inside an image element.
func SVGIsFlatPathArtwork(svg []byte) bool {
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil {
		return false
	}
	root := document.Root()
	if root == nil {
		return false
	}
	viewBox, ok := parseSVGViewBox(root.SelectAttrValue("viewBox", ""))
	if !ok || viewBox[2] <= 0 || viewBox[3] <= 0 {
		return false
	}
	paths := 0
	for _, child := range root.ChildElements() {
		switch child.Tag {
		case "path":
			paths++
		case "text":
		default:
			return false
		}
	}
	return paths > 0
}

func pathCell(id, width, height int, commands string, source *etree.Element) string {
	fill := source.SelectAttrValue("fill", "none")
	stroke := source.SelectAttrValue("stroke", "none")
	strokeWidth := source.SelectAttrValue("stroke-width", "1")
	shape := fmt.Sprintf(`<shape name="wedraw-path" w="%d" h="%d" aspect="variable" strokewidth="inherit"><foreground>`, width, height)
	if fill != "none" {
		shape += `<fillcolor color="fill"/>`
	}
	if stroke != "none" {
		shape += fmt.Sprintf(`<strokecolor color="stroke"/><strokewidth width="%s" fixed="1"/>`, xmlEscapeAttr(strokeWidth))
	}
	shape += `<path>` + commands + `</path>`
	if fill != "none" && stroke != "none" {
		shape += `<fillstroke/>`
	} else if fill != "none" {
		shape += `<fill/>`
	} else {
		shape += `<stroke/>`
	}
	shape += `</foreground></shape>`
	compressed, err := compressStencil(shape)
	if err != nil {
		return ""
	}
	style := nativeColorStyle("shape=stencil("+compressed+");html=1;", fill, stroke)
	return fmt.Sprintf(`<mxCell id="%d" value="" style="%s" vertex="1" parent="2"><mxGeometry x="0" y="0" width="%d" height="%d" as="geometry"/></mxCell>`, id, xmlEscapeAttr(style), width, height)
}

func nativeColorStyle(style, fill, stroke string) string {
	if fill != "none" && fill != "" {
		style += "fillColor=" + fill + ";"
	}
	if stroke != "none" && stroke != "" {
		style += "strokeColor=" + stroke + ";"
	}
	return style
}

func svgPathToStencil(value string, viewBox [4]float64, width, height int) (string, error) {
	tokens := svgPathToken.FindAllString(value, -1)
	if len(tokens) == 0 {
		return "", fmt.Errorf("path has no commands")
	}
	index := 0
	command := byte(0)
	x, y := 0.0, 0.0
	startX, startY := 0.0, 0.0
	var result strings.Builder
	readNumber := func() (float64, error) {
		if index >= len(tokens) || len(tokens[index]) == 1 && strings.ContainsRune("AaCcHhLlMmQqSsTtVvZz", rune(tokens[index][0])) {
			return 0, fmt.Errorf("missing path coordinate")
		}
		parsed, err := strconv.ParseFloat(tokens[index], 64)
		index++
		return parsed, err
	}
	mapPoint := func(px, py float64) (float64, float64) {
		return (px - viewBox[0]) * float64(width) / viewBox[2], (py - viewBox[1]) * float64(height) / viewBox[3]
	}
	for index < len(tokens) {
		if len(tokens[index]) == 1 && strings.ContainsRune("AaCcHhLlMmQqSsTtVvZz", rune(tokens[index][0])) {
			command = tokens[index][0]
			index++
		}
		if command == 0 {
			return "", fmt.Errorf("path starts without a command")
		}
		relative := command >= 'a' && command <= 'z'
		switch command {
		case 'M', 'm', 'L', 'l':
			px, err := readNumber()
			if err != nil {
				return "", err
			}
			py, err := readNumber()
			if err != nil {
				return "", err
			}
			if relative {
				px += x
				py += y
			}
			mx, my := mapPoint(px, py)
			if command == 'M' || command == 'm' && result.Len() == 0 {
				result.WriteString(fmt.Sprintf(`<move x="%s" y="%s"/>`, fmtNum(mx), fmtNum(my)))
				startX, startY = px, py
			} else {
				result.WriteString(fmt.Sprintf(`<line x="%s" y="%s"/>`, fmtNum(mx), fmtNum(my)))
			}
			x, y = px, py
			if command == 'M' {
				command = 'L'
			} else if command == 'm' {
				command = 'l'
			}
		case 'H', 'h':
			px, err := readNumber()
			if err != nil {
				return "", err
			}
			if relative {
				px += x
			}
			mx, my := mapPoint(px, y)
			result.WriteString(fmt.Sprintf(`<line x="%s" y="%s"/>`, fmtNum(mx), fmtNum(my)))
			x = px
		case 'V', 'v':
			py, err := readNumber()
			if err != nil {
				return "", err
			}
			if relative {
				py += y
			}
			mx, my := mapPoint(x, py)
			result.WriteString(fmt.Sprintf(`<line x="%s" y="%s"/>`, fmtNum(mx), fmtNum(my)))
			y = py
		case 'C', 'c':
			values := make([]float64, 6)
			for i := range values {
				var err error
				values[i], err = readNumber()
				if err != nil {
					return "", err
				}
			}
			if relative {
				for i := 0; i < 6; i += 2 {
					values[i] += x
					values[i+1] += y
				}
			}
			x1, y1 := mapPoint(values[0], values[1])
			x2, y2 := mapPoint(values[2], values[3])
			px, py := mapPoint(values[4], values[5])
			result.WriteString(fmt.Sprintf(`<curve x1="%s" y1="%s" x2="%s" y2="%s" x3="%s" y3="%s"/>`, fmtNum(x1), fmtNum(y1), fmtNum(x2), fmtNum(y2), fmtNum(px), fmtNum(py)))
			x, y = values[4], values[5]
		case 'Q', 'q':
			values := make([]float64, 4)
			for i := range values {
				var err error
				values[i], err = readNumber()
				if err != nil {
					return "", err
				}
			}
			if relative {
				for i := 0; i < 4; i += 2 {
					values[i] += x
					values[i+1] += y
				}
			}
			cx, cy := mapPoint(values[0], values[1])
			px, py := mapPoint(values[2], values[3])
			result.WriteString(fmt.Sprintf(`<quad x1="%s" y1="%s" x2="%s" y2="%s"/>`, fmtNum(cx), fmtNum(cy), fmtNum(px), fmtNum(py)))
			x, y = values[2], values[3]
		case 'Z', 'z':
			result.WriteString(`<close/>`)
			x, y = startX, startY
		default:
			return "", fmt.Errorf("unsupported SVG path command %c", command)
		}
	}
	return result.String(), nil
}

func parseSVGViewBox(value string) ([4]float64, bool) {
	var result [4]float64
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if len(parts) != 4 {
		return result, false
	}
	for i, part := range parts {
		parsed, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return result, false
		}
		result[i] = parsed
	}
	return result, true
}

func fmtNum(value float64) string { return strconv.FormatFloat(value, 'f', 4, 64) }
