package visio

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

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
