package visio

import (
	"strconv"

	"github.com/wijnberg-net/vsdx-go/vsdx"
)

// Anchor is a normalized draw.io connection point.
type Anchor struct {
	X float64
	Y float64
}

// Anchors extracts Visio Connection rows and maps their local coordinates to
// draw.io's 0..1 coordinates. Visio uses a lower-left origin; draw.io uses a
// top-left origin, so Y is flipped here.
func Anchors(shape *vsdx.Shape) ([]Anchor, error) {
	if shape == nil || shape.Width() <= 0 || shape.Height() <= 0 {
		return nil, nil
	}
	var result []Anchor
	for _, section := range shape.XML().SelectElements("Section") {
		if section.SelectAttrValue("N", "") != "Connection" {
			continue
		}
		for _, row := range section.SelectElements("Row") {
			values := make(map[string]string)
			for _, cell := range row.SelectElements("Cell") {
				values[cell.SelectAttrValue("N", "")] = cell.SelectAttrValue("V", "")
			}
			x, errX := strconv.ParseFloat(values["X"], 64)
			y, errY := strconv.ParseFloat(values["Y"], 64)
			if errX != nil || errY != nil {
				continue
			}
			result = append(result, Anchor{
				X: x / shape.Width(),
				Y: 1 - y/shape.Height(),
			})
		}
	}
	return result, nil
}

// MasterAnchors returns anchors from the first master shape that declares a
// connection section. Cisco masters normally keep the section on the primary
// shape while child shapes carry the visual details.
func MasterAnchors(master *vsdx.Page) ([]Anchor, error) {
	if master == nil {
		return nil, nil
	}
	// Connection sections normally live on the top-level master shape. Avoid
	// rebuilding the complete recursive shape list for every master; that list
	// is expensive for large Cisco stencils.
	for _, shape := range master.ChildShapes() {
		anchors, err := Anchors(shape)
		if err != nil {
			return nil, err
		}
		if len(anchors) > 0 {
			return anchors, nil
		}
	}
	// Keep a compatibility fallback for masters whose connection section is
	// attached to a nested child rather than the primary shape.
	for _, shape := range master.AllShapes() {
		anchors, err := Anchors(shape)
		if err != nil {
			return nil, err
		}
		if len(anchors) > 0 {
			return anchors, nil
		}
	}
	return nil, nil
}
