package library

import (
	"compress/flate"
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

func TestWriteProducesImportableLibraryWrapper(t *testing.T) {
	data, err := Write([]Entry{NewSVGEntry("Router", 120, 80, []byte("<svg/>"))}, "Cisco")
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		XMLName xml.Name
		Text    string `xml:",chardata"`
	}
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root.XMLName.Local != "mxlibrary" {
		t.Fatalf("root = %q, want mxlibrary", root.XMLName.Local)
	}
	if !strings.Contains(root.Text, `"title": "Router"`) {
		t.Fatalf("library JSON does not contain entry title: %s", root.Text)
	}
}

func TestPointsStyleUsesDrawioConnectionPointSyntax(t *testing.T) {
	style := PointsStyle([][2]float64{{0, 0.5}, {1, 0.5}})
	if style != "points=[[0.000000,0.500000],[1.000000,0.500000]];" {
		t.Fatalf("style = %q", style)
	}
}

func TestNewSVGStencilEntryUsesXMLModel(t *testing.T) {
	entry, err := NewSVGStencilEntry("Router", 120, 80, []byte("<svg/>"), PointsStyle([][2]float64{{0, 0.5}}))
	if err != nil {
		t.Fatal(err)
	}
	if entry.XML == "" || entry.Data != "" {
		t.Fatalf("entry must use xml only: %#v", entry)
	}
	if !strings.Contains(entry.XML, "shape=stencil(") || !strings.Contains(entry.XML, "points=[[0.000000,0.500000]]") {
		t.Fatalf("entry does not contain inline stencil and points: %s", entry.XML)
	}
	start := strings.Index(entry.XML, "shape=stencil(") + len("shape=stencil(")
	end := strings.Index(entry.XML[start:], ");html=1;") + start
	compressed, err := base64.StdEncoding.DecodeString(entry.XML[start:end])
	if err != nil {
		t.Fatal(err)
	}
	reader := flate.NewReader(strings.NewReader(string(compressed)))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	unescaped, err := url.PathUnescape(string(decoded))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unescaped, "<shape") || !strings.Contains(unescaped, "data:image/svg+xml;base64") {
		t.Fatalf("stencil payload did not round-trip: %s", unescaped)
	}
}

func TestNewSVGGroupEntrySeparatesEditableText(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 100 100"><path d="M0 0L100 100"/><text x="50" y="90" font-size="10">Router</text></svg>`)
	entry, err := NewSVGGroupEntry("Router", 100, 100, svg, PointsStyle([][2]float64{{0.5, 0.5}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry.XML, `style="group;html=1;points=[[0.500000,0.500000]];"`) {
		t.Fatalf("group cell is missing connection points: %s", entry.XML)
	}
	if !strings.Contains(entry.XML, `id="3" value="" style="shape=image`) {
		t.Fatalf("group is missing the SVG artwork child: %s", entry.XML)
	}
	if !strings.Contains(entry.XML, `id="4" value="Router" style="text;`) {
		t.Fatalf("group is missing the editable text child: %s", entry.XML)
	}
	if strings.Contains(entry.XML, `<text`) {
		t.Fatal("text was not removed from the artwork SVG")
	}
	assertCellTree(t, entry.XML)
}

func TestNewSVGUngroupEntryDecomposesSimplePathAndText(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 100 100"><path d="M0 0 L100 0 L100 100 Z" fill="#ffffff" stroke="#000000" stroke-width="2"/><text x="50" y="90" font-size="10">Router</text></svg>`)
	entry, err := NewSVGUngroupEntry("Router", 100, 100, svg, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry.XML, `shape=stencil(`) {
		t.Fatalf("ungroup entry has no native path stencil: %s", entry.XML)
	}
	if !strings.Contains(entry.XML, `fillColor=#ffffff`) || !strings.Contains(entry.XML, `strokeColor=#000000`) {
		t.Fatal("native path colors are not exposed as standard draw.io styles")
	}
	if !strings.Contains(entry.XML, `value="Router" style="text;`) {
		t.Fatalf("ungroup entry has no editable text cell: %s", entry.XML)
	}
	if strings.Contains(entry.XML, `shape=image`) {
		t.Fatal("simple path was unexpectedly replaced by an SVG fallback image")
	}
	assertCellTree(t, entry.XML)
}

func TestNewSVGUngroupEntryFallsBackForUnsupportedPath(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 100 100"><path d="M0 0 A10 10 0 0 1 20 20" fill="none" stroke="#000000"/><text x="50" y="90">Arc</text></svg>`)
	entry, err := NewSVGUngroupEntry("Arc", 100, 100, svg, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry.XML, `shape=image`) {
		t.Fatal("unsupported arc path was not preserved as an SVG fallback")
	}
	if !strings.Contains(entry.XML, `value="Arc" style="text;`) {
		t.Fatal("fallback group lost the editable text cell")
	}
	assertCellTree(t, entry.XML)
}

func TestNativePathStylesKeepFillOnlyAndStrokeOnlySemantics(t *testing.T) {
	fillOnly := etree.NewElement("path")
	fillOnly.CreateAttr("fill", "#ffffff")
	fillOnly.CreateAttr("stroke", "none")
	fillOnly.CreateAttr("stroke-width", "1")
	strokeOnly := etree.NewElement("path")
	strokeOnly.CreateAttr("fill", "none")
	strokeOnly.CreateAttr("stroke", "#000000")
	strokeOnly.CreateAttr("stroke-width", "1")
	fillCell := pathCell(3, 100, 100, `<move x="0" y="0"/><line x="100" y="100"/>`, fillOnly)
	strokeCell := pathCell(4, 100, 100, `<move x="0" y="0"/><line x="100" y="100"/>`, strokeOnly)
	if !strings.Contains(fillCell, `fillColor=#ffffff`) || strings.Contains(fillCell, `strokeColor=`) {
		t.Fatalf("fill-only cell has incorrect editable colors: %s", fillCell)
	}
	if strings.Contains(strokeCell, `fillColor=`) || !strings.Contains(strokeCell, `strokeColor=#000000`) {
		t.Fatalf("stroke-only cell has incorrect editable colors: %s", strokeCell)
	}
}

func TestNewRenderTreeUngroupEntryPreservesNestedParents(t *testing.T) {
	root := &vsdx.RenderNode{
		Visible: true,
		Children: []*vsdx.RenderNode{
			{
				Visible: true,
				Children: []*vsdx.RenderNode{{
					Visible:  true,
					Geometry: []*vsdx.ResolvedPath{{D: "M0 0 L10 10", Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 1}},
				}},
			},
			{Visible: true, Text: &vsdx.ResolvedText{Content: "Nested", X: 10, Y: 20, FontSize: 10, Fill: "#000000"}},
		},
	}
	entry, err := NewRenderTreeUngroupEntry("Nested", 100, 100, 100, 100, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry.XML, `id="3" value="" style="group;" vertex="1" parent="2"`) {
		t.Fatalf("nested group was not emitted: %s", entry.XML)
	}
	if !strings.Contains(entry.XML, `id="4" value="" style="group;" vertex="1" parent="3"`) {
		t.Fatalf("second nested group was not emitted: %s", entry.XML)
	}
	if !strings.Contains(entry.XML, `parent="4"`) {
		t.Fatalf("deep nested children do not reference the deepest group: %s", entry.XML)
	}
	assertCellTree(t, entry.XML)
}

func TestNewFlatRenderTreeUngroupEntryExposesElementsAtOuterLevel(t *testing.T) {
	root := &vsdx.RenderNode{
		Visible: true,
		Children: []*vsdx.RenderNode{{
			Visible:  true,
			Geometry: []*vsdx.ResolvedPath{{D: "M0 0 L10 10", Fill: "#ffffff", Stroke: "#000000", StrokeWidth: 1}},
		}},
	}
	entry, err := NewFlatRenderTreeUngroupEntry("Flat", 100, 100, 100, 100, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entry.XML, `style="group;" vertex="1" parent="2"`) {
		t.Fatal("flat ungroup output contains an extra structural group")
	}
	if !strings.Contains(entry.XML, `shape=stencil(`) {
		t.Fatal("flat ungroup output lost the native path")
	}
	assertCellTree(t, entry.XML)
}

func assertCellTree(t *testing.T, model string) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(model))
	ids := map[string]bool{}
	parents := make([]string, 0)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid mxGraphModel XML: %v", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "mxCell" {
			continue
		}
		var id, parent string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "id":
				id = attr.Value
			case "parent":
				parent = attr.Value
			}
		}
		if id == "" || ids[id] {
			t.Fatalf("cell IDs are not unique: %q", id)
		}
		ids[id] = true
		if parent != "" {
			parents = append(parents, parent)
		}
	}
	for _, parent := range parents {
		if !ids[parent] {
			t.Fatalf("cell references missing parent %q", parent)
		}
	}
}

func TestEncodeURIComponentDoesNotUsePlusForSpaces(t *testing.T) {
	got := encodeURIComponent("a b+c/é")
	want := "a%20b%2Bc%2F%C3%A9"
	if got != want {
		t.Fatalf("encoded = %q, want %q", got, want)
	}
}
