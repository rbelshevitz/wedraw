package visio

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

var svgBoundsToken = regexp.MustCompile(`[AaCcHhLlMmQqSsTtVvZz]|[-+]?(?:\d*\.\d+|\d+\.?)(?:[eE][-+]?\d+)?`)

// NormalizeSVGViewBox expands a renderer viewBox when emitted paths extend
// beyond it. Some server stencils use nested coordinates whose Y range is
// much larger than the primary shape's declared height.
func NormalizeSVGViewBox(svg []byte, outputWidth, outputHeight float64) ([]byte, float64, float64, error) {
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil {
		return svg, outputWidth, outputHeight, fmt.Errorf("parse SVG bounds: %w", err)
	}
	root := document.Root()
	if root == nil {
		return svg, outputWidth, outputHeight, nil
	}
	viewBox, ok := parseViewBox(root.SelectAttrValue("viewBox", ""))
	if !ok || viewBox[2] <= 0 || viewBox[3] <= 0 || outputWidth <= 0 || outputHeight <= 0 {
		return svg, outputWidth, outputHeight, nil
	}

	minX := viewBox[0]
	minY := viewBox[1]
	maxX := viewBox[0] + viewBox[2]
	maxY := viewBox[1] + viewBox[3]
	// Paths inside a nested SVG use that nested element's viewBox. Treating
	// their raw coordinates as outer-canvas coordinates can expand the parent
	// viewBox by 100x or more and make the actual figure appear tiny in the
	// upper-left corner. The compositor already positions nested SVG layers;
	// only inspect paths that belong to the outer coordinate system.
	collectOuterSVGPaths(root, func(path *etree.Element) {
		boundsPath(path.SelectAttrValue("d", ""), &minX, &minY, &maxX, &maxY)
	})
	if minX >= viewBox[0] && minY >= viewBox[1] && maxX <= viewBox[0]+viewBox[2] && maxY <= viewBox[1]+viewBox[3] {
		return svg, outputWidth, outputHeight, nil
	}

	const padding = 0.5
	minX -= padding
	minY -= padding
	maxX += padding
	maxY += padding
	viewBox = [4]float64{minX, minY, maxX - minX, maxY - minY}
	root.RemoveAttr("viewBox")
	root.CreateAttr("viewBox", formatViewBox(viewBox))
	updated, err := document.WriteToBytes()
	if err != nil {
		return svg, outputWidth, outputHeight, fmt.Errorf("write normalized SVG bounds: %w", err)
	}
	return updated, outputWidth, outputWidth * viewBox[3] / viewBox[2], nil
}

func collectOuterSVGPaths(root *etree.Element, visit func(*etree.Element)) {
	if root == nil || visit == nil {
		return
	}
	var walk func(*etree.Element)
	walk = func(elem *etree.Element) {
		for _, child := range elem.ChildElements() {
			if child.Tag == "svg" {
				continue
			}
			if child.Tag == "path" {
				visit(child)
			}
			walk(child)
		}
	}
	walk(root)
}

func boundsPath(value string, minX, minY, maxX, maxY *float64) {
	tokens := svgBoundsToken.FindAllString(value, -1)
	if len(tokens) == 0 {
		return
	}
	command := byte(0)
	i := 0
	x, y := 0.0, 0.0
	startX, startY := 0.0, 0.0
	for i < len(tokens) {
		if len(tokens[i]) == 1 && strings.ContainsRune("AaCcHhLlMmQqSsTtVvZz", rune(tokens[i][0])) {
			command = tokens[i][0]
			i++
			if command == 'Z' || command == 'z' {
				x, y = startX, startY
			}
			continue
		}
		if command == 0 {
			i++
			continue
		}
		relative := command >= 'a' && command <= 'z'
		upper := command
		if relative {
			upper -= 'a' - 'A'
		}
		need := map[byte]int{'M': 2, 'L': 2, 'T': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'A': 7}[upper]
		if need == 0 || i+need > len(tokens) {
			break
		}
		values := make([]float64, need)
		valid := true
		for j := range values {
			values[j], _ = strconv.ParseFloat(tokens[i+j], 64)
			if math.IsNaN(values[j]) {
				valid = false
			}
		}
		if !valid {
			break
		}
		point := func(px, py float64) {
			if px < *minX {
				*minX = px
			}
			if px > *maxX {
				*maxX = px
			}
			if py < *minY {
				*minY = py
			}
			if py > *maxY {
				*maxY = py
			}
		}
		add := func(v float64, base float64) float64 {
			if relative {
				return base + v
			}
			return v
		}
		switch upper {
		case 'M', 'L', 'T':
			x, y = add(values[0], x), add(values[1], y)
			point(x, y)
			if upper == 'M' {
				startX, startY = x, y
				command = byte('l')
				if !relative {
					command = 'L'
				}
			}
		case 'H':
			x = add(values[0], x)
			point(x, y)
		case 'V':
			y = add(values[0], y)
			point(x, y)
		case 'C':
			for j := 0; j < 6; j += 2 {
				point(add(values[j], x), add(values[j+1], y))
			}
			x, y = add(values[4], x), add(values[5], y)
		case 'S', 'Q':
			for j := 0; j < 4; j += 2 {
				point(add(values[j], x), add(values[j+1], y))
			}
			x, y = add(values[2], x), add(values[3], y)
		case 'A':
			rx, ry := math.Abs(values[0]), math.Abs(values[1])
			endX, endY := add(values[5], x), add(values[6], y)
			point(x-rx, y-ry)
			point(x+rx, y+ry)
			point(endX-rx, endY-ry)
			point(endX+rx, endY+ry)
			x, y = endX, endY
		}
		i += need
	}
}

// PrimaryTextMargin returns the SVG-space margin that vsdx-go currently adds
// to an explicitly positioned Visio text block. Visio's TxtPinX is already
// the text reference point, so adding the left margin shifts centered labels.
func PrimaryTextMargin(master *vsdx.Page, renderedWidth float64) float64 {
	if master == nil || renderedWidth <= 0 {
		return 0
	}
	shapes := master.ChildShapes()
	if len(shapes) == 0 {
		return 0
	}
	shape := shapes[0]
	if strings.TrimSpace(shape.Text()) == "" || shape.CellValue("TxtPinX") == "" {
		return 0
	}
	shapeWidth := math.Abs(shape.Width())
	if shapeWidth <= 0 {
		return 0
	}
	const defaultVisioTextMargin = 4.0 / 72.0 // 4 pt, in Visio inches
	return defaultVisioTextMargin / shapeWidth * renderedWidth
}

// ShiftSVGText moves explicit text-block positions without changing the
// text-anchor. It also adjusts tspan x attributes used by multiline labels.
func ShiftSVGText(svg []byte, shift float64) ([]byte, error) {
	if shift == 0 {
		return svg, nil
	}
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil {
		return svg, fmt.Errorf("parse rendered SVG text: %w", err)
	}
	root := document.Root()
	if root == nil {
		return svg, nil
	}
	for _, text := range root.FindElements(".//text") {
		shiftSVGX(text, shift)
		for _, span := range text.FindElements(".//tspan") {
			shiftSVGX(span, shift)
		}
	}
	updated, err := document.WriteToBytes()
	if err != nil {
		return svg, fmt.Errorf("write shifted SVG text: %w", err)
	}
	return updated, nil
}

func shiftSVGX(element *etree.Element, shift float64) {
	attr := element.SelectAttr("x")
	if attr == nil {
		return
	}
	x, err := strconv.ParseFloat(attr.Value, 64)
	if err != nil {
		return
	}
	attr.Value = strconv.FormatFloat(x-shift, 'f', 4, 64)
}

// ExpandSVGForText extends an SVG viewBox when Visio text is emitted outside
// the geometry bounds. Some stencils place the label below the visible icon;
// leaving the renderer's original viewBox unchanged clips that label.
func ExpandSVGForText(svg []byte, outputWidth, outputHeight float64) ([]byte, float64, float64, error) {
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil {
		return svg, outputWidth, outputHeight, fmt.Errorf("parse rendered SVG: %w", err)
	}
	root := document.Root()
	if root == nil {
		return svg, outputWidth, outputHeight, nil
	}
	viewBox, ok := parseViewBox(root.SelectAttrValue("viewBox", ""))
	if !ok || viewBox[2] <= 0 || viewBox[3] <= 0 || outputHeight <= 0 {
		return svg, outputWidth, outputHeight, nil
	}

	oldMaxY := viewBox[1] + viewBox[3]
	maxY := oldMaxY
	for _, text := range root.FindElements(".//text") {
		y, err := strconv.ParseFloat(text.SelectAttrValue("y", ""), 64)
		if err != nil {
			continue
		}
		fontSize, err := strconv.ParseFloat(text.SelectAttrValue("font-size", ""), 64)
		if err != nil || fontSize <= 0 {
			fontSize = 12
		}
		lineCount := len(text.SelectElements("tspan"))
		if lineCount < 1 {
			lineCount = 1
		}
		// Text baselines are normally at y. Include a small glyph descent and
		// the line spacing of every additional tspan.
		bottom := y + fontSize*(0.25+1.2*float64(lineCount-1))
		if bottom > maxY {
			maxY = bottom
		}
	}
	if maxY <= oldMaxY {
		return svg, outputWidth, outputHeight, nil
	}

	scale := outputHeight / viewBox[3]
	if scale <= 0 {
		return svg, outputWidth, outputHeight, nil
	}
	newHeight := outputHeight + (maxY-oldMaxY)*scale
	viewBox[3] = maxY - viewBox[1]
	root.RemoveAttr("viewBox")
	root.CreateAttr("viewBox", formatViewBox(viewBox))
	updated, err := document.WriteToBytes()
	if err != nil {
		return svg, outputWidth, outputHeight, fmt.Errorf("write expanded SVG: %w", err)
	}
	return updated, outputWidth, math.Ceil(newHeight), nil
}

func parseViewBox(value string) ([4]float64, bool) {
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

func formatViewBox(value [4]float64) string {
	parts := make([]string, len(value))
	for i, item := range value {
		parts[i] = strconv.FormatFloat(item, 'f', 4, 64)
	}
	return strings.Join(parts, " ")
}
