package library

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// Entry is one reusable item in a draw.io custom library.
type Entry struct {
	Title  string `json:"title"`
	Width  int    `json:"w"`
	Height int    `json:"h"`
	XML    string `json:"xml,omitempty"`
	Data   string `json:"data,omitempty"`
	Aspect string `json:"aspect,omitempty"`
	Style  string `json:"style,omitempty"`
	Tags   string `json:"tags,omitempty"`
}

var labelGapPixels = 15

// SetLabelGap controls the vertical gap between artwork and the editable
// label in the generated library. The CLI is single-threaded, so this setting
// is intentionally process-wide for one conversion run.
func SetLabelGap(pixels int) {
	if pixels < 0 {
		pixels = 0
	}
	labelGapPixels = pixels
}

// NewSVGStencilEntry creates a reusable draw.io entry whose cell uses an
// inline stencil. The stencil contains the complete source SVG as an image,
// so complex Visio geometry is not reduced to guessed primitives.
func NewSVGStencilEntry(title string, width, height int, svg []byte, points string) (Entry, error) {
	if width < 1 || height < 1 {
		return Entry{}, fmt.Errorf("invalid stencil size %dx%d", width, height)
	}
	dataURI := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg)
	shapeXML := fmt.Sprintf(
		`<shape name="wedraw" w="%d" h="%d" aspect="fixed" strokewidth="inherit"><foreground><image src="%s" x="0" y="0" w="%d" h="%d"/></foreground></shape>`,
		width, height, xmlEscapeAttr(dataURI), width, height)
	compressed, err := compressStencil(shapeXML)
	if err != nil {
		return Entry{}, err
	}
	style := "shape=stencil(" + compressed + ");html=1;"
	style += points
	model := fmt.Sprintf(
		`<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="" style="%s" vertex="1" parent="1"><mxGeometry width="%d" height="%d" as="geometry"/></mxCell></root></mxGraphModel>`,
		xmlEscapeAttr(style), width, height)
	return Entry{Title: title, Width: width, Height: height, XML: model}, nil
}

// NewSVGGroupEntry creates the common hybrid representation used by both
// conversion modes: artwork remains a faithful SVG child while visible SVG
// text becomes separate editable draw.io text cells. The ungroup mode can
// later add native path children where conversion is proven safe.
func NewSVGGroupEntry(title string, width, height int, svg []byte, points string) (Entry, error) {
	if width < 1 || height < 1 {
		return Entry{}, fmt.Errorf("invalid group size %dx%d", width, height)
	}
	document := etree.NewDocument()
	if err := document.ReadFromBytes(svg); err != nil {
		return Entry{}, fmt.Errorf("parse SVG for group mode: %w", err)
	}
	root := document.Root()
	if root == nil {
		return Entry{}, fmt.Errorf("SVG for group mode has no root element")
	}
	texts := root.FindElements(".//text")
	textCells := make([]string, 0, len(texts))
	for index, text := range texts {
		value := strings.TrimSpace(svgTextContent(text))
		if value == "" {
			continue
		}
		textCells = append(textCells, groupTextCell(index+4, value, width, height, text))
		if parent := text.Parent(); parent != nil {
			parent.RemoveChild(text)
		}
	}
	cleanSVG, err := document.WriteToBytes()
	if err != nil {
		return Entry{}, fmt.Errorf("write SVG for group mode: %w", err)
	}
	image := base64.StdEncoding.EncodeToString(cleanSVG)
	groupStyle := "group;html=1;" + points
	model := fmt.Sprintf(
		`<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="" style="%s" vertex="1" parent="1"><mxGeometry width="%d" height="%d" as="geometry"/></mxCell><mxCell id="3" value="" style="shape=image;html=1;image=data:image/svg+xml,%s;aspect=fixed;imageAspect=0;" vertex="1" parent="2"><mxGeometry x="0" y="0" width="%d" height="%d" as="geometry"/></mxCell>%s</root></mxGraphModel>`,
		xmlEscapeAttr(groupStyle), width, height, image, width, height, strings.Join(textCells, ""))
	return Entry{Title: title, Width: width, Height: height, XML: model}, nil
}

func svgTextContent(element *etree.Element) string {
	var result strings.Builder
	var visit func(*etree.Element)
	visit = func(current *etree.Element) {
		if text := current.Text(); text != "" {
			result.WriteString(text)
		}
		for _, child := range current.ChildElements() {
			if result.Len() > 0 && child.Tag == "tspan" {
				result.WriteByte('\n')
			}
			visit(child)
		}
	}
	visit(element)
	return result.String()
}

func groupTextCell(id int, value string, width, height int, text *etree.Element) string {
	fontSize := 12.0
	if parsed, err := strconv.ParseFloat(text.SelectAttrValue("font-size", ""), 64); err == nil && parsed > 0 {
		fontSize = parsed * 0.72
	}
	if fontSize < 8 {
		fontSize = 8
	}
	if fontSize > 18 {
		fontSize = 18
	}
	fontFamily := text.SelectAttrValue("font-family", "Arial")
	fontColor := text.SelectAttrValue("fill", "#000000")
	style := "text;html=1;whiteSpace=wrap;align=center;verticalAlign=bottom;spacingBottom=1;" +
		"fontColor=" + fontColor + ";fontFamily=" + fontFamily + ";fontSize=" +
		strconv.Itoa(int(math.Round(fontSize))) + ";"
	labelHeight := int(math.Ceil(fontSize * 1.35))
	if labelHeight < 18 {
		labelHeight = 18
	}
	y := height - labelHeight - labelGapPixels
	if y < 0 {
		y = 0
	}
	return fmt.Sprintf(`<mxCell id="%d" value="%s" style="%s" vertex="1" parent="2"><mxGeometry x="0" y="%d" width="%d" height="%d" as="geometry"/></mxCell>`, id, xmlEscapeAttr(value), xmlEscapeAttr(style), y, width, labelHeight)
}

// NewSVGEntry creates a self-contained SVG image entry. It is intentionally
// separate from native stencil output until anchors are mapped reliably.
func NewSVGEntry(title string, width, height int, svg []byte) Entry {
	return NewSVGEntryWithStyle(title, width, height, svg, "")
}

func NewSVGEntryWithStyle(title string, width, height int, svg []byte, style string) Entry {
	encoded := base64.StdEncoding.EncodeToString(svg)
	return Entry{
		Title:  title,
		Width:  width,
		Height: height,
		Data:   "data:image/svg+xml;base64," + encoded,
		Aspect: "fixed",
		Style:  style,
	}
}

func PointsStyle(points [][2]float64) string {
	if len(points) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("points=[[")
	for i, point := range points {
		if i > 0 {
			b.WriteString("],[")
		}
		b.WriteString(strconv.FormatFloat(point[0], 'f', 6, 64))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(point[1], 'f', 6, 64))
	}
	b.WriteString("]];")
	return b.String()
}

// Write serializes a draw.io custom library. The JSON is kept readable and
// XML-escaped so the result is easy to inspect and import.
func Write(entries []Entry, title string) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("library has no entries")
	}
	payload, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal library entries: %w", err)
	}
	var out bytes.Buffer
	writer, err := NewWriter(&out, title)
	if err != nil {
		return nil, err
	}
	if err := writer.writeJSON(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Writer streams a custom library so large stencils do not require keeping
// all rendered SVG and JSON entries in memory at once.
type Writer struct {
	out    io.Writer
	first  bool
	closed bool
}

func NewWriter(out io.Writer, title string) (*Writer, error) {
	if out == nil {
		return nil, fmt.Errorf("library output is nil")
	}
	if _, err := io.WriteString(out, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<mxlibrary"); err != nil {
		return nil, fmt.Errorf("write library header: %w", err)
	}
	if title != "" {
		if _, err := io.WriteString(out, " title=\""+xmlEscapeAttr(title)+"\""); err != nil {
			return nil, fmt.Errorf("write library title: %w", err)
		}
	}
	if _, err := io.WriteString(out, ">["); err != nil {
		return nil, fmt.Errorf("write library start: %w", err)
	}
	return &Writer{out: out, first: true}, nil
}

func (w *Writer) WriteEntry(entry Entry) error {
	if w == nil || w.closed {
		return fmt.Errorf("library writer is closed")
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal library entry %q: %w", entry.Title, err)
	}
	return w.writeJSON(payload)
}

func (w *Writer) writeJSON(payload []byte) error {
	if !w.first {
		if _, err := io.WriteString(w.out, ","); err != nil {
			return fmt.Errorf("write library separator: %w", err)
		}
	}
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, payload); err != nil {
		return fmt.Errorf("escape library JSON: %w", err)
	}
	if _, err := w.out.Write(escaped.Bytes()); err != nil {
		return fmt.Errorf("write library entry: %w", err)
	}
	w.first = false
	return nil
}

func (w *Writer) Close() error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true
	if _, err := io.WriteString(w.out, "]</mxlibrary>\n"); err != nil {
		return fmt.Errorf("write library footer: %w", err)
	}
	return nil
}

func SanitizeTitle(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unnamed shape"
	}
	return value
}

func compressStencil(value string) (string, error) {
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		return "", fmt.Errorf("create stencil compressor: %w", err)
	}
	if _, err := writer.Write([]byte(encodeURIComponent(value))); err != nil {
		return "", fmt.Errorf("compress stencil: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close stencil compressor: %w", err)
	}
	return base64.StdEncoding.EncodeToString(compressed.Bytes()), nil
}

func xmlEscapeAttr(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"\"", "&quot;",
		"<", "&lt;",
		">", "&gt;",
		"'", "&apos;",
	).Replace(value)
}

// encodeURIComponent matches the JavaScript encoding used by draw.io's
// Graph.compress/decompress path. In particular, spaces become %20 rather
// than '+', and '+' itself becomes %2B.
func encodeURIComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || strings.ContainsRune("-_.!~*'()", rune(c)) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0F])
	}
	return b.String()
}
