package visio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beevik/etree"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

// ExportMasterDebug writes the source master XML, each recovered Foreign SVG
// layer, and the composed SVG for visual comparison.
func ExportMasterDebug(stencil *vsdx.Stencil, master *vsdx.Page, outputDir string) error {
	if stencil == nil || master == nil {
		return fmt.Errorf("master is not available")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	if data, err := master.XML().WriteToBytes(); err == nil {
		if err := os.WriteFile(filepath.Join(outputDir, "master.xml"), data, 0o644); err != nil {
			return err
		}
	}
	artwork, _, _, found, err := MasterEmbeddedArtwork(stencil, master)
	if err != nil {
		return err
	}
	if found {
		if err := os.WriteFile(filepath.Join(outputDir, "composite.svg"), artwork, 0o644); err != nil {
			return err
		}
	}
	debugRelsPath := filepath.ToSlash(filepath.Join("visio", "masters", "_rels", "master"+master.PageID()+".xml.rels"))
	if rels, ok := stencil.ZipFileContents[debugRelsPath]; ok {
		_ = os.WriteFile(filepath.Join(outputDir, "relationships.xml"), rels, 0o644)
	}
	_ = os.WriteFile(filepath.Join(outputDir, "page-id.txt"), []byte(master.PageID()+"\n"+debugRelsPath+"\n"), 0o644)
	if children := master.ChildShapes(); len(children) > 0 {
		if rendered, renderErr := vsdx.ShapeToSVG(children[0], vsdx.WithPrecision(2)); renderErr == nil && rendered != nil {
			if err := os.WriteFile(filepath.Join(outputDir, "render-tree.svg"), rendered.SVG, 0o644); err != nil {
				return err
			}
		}
	}
	root := master.XML().Root()
	relsPath := filepath.ToSlash(filepath.Join("visio", "masters", "_rels", "master"+master.PageID()+".xml.rels"))
	relsDoc := etree.NewDocument()
	if data, ok := stencil.ZipFileContents[relsPath]; ok {
		if err := relsDoc.ReadFromBytes(data); err != nil {
			return err
		}
	}
	targets := make(map[string]string)
	if relsDoc.Root() != nil {
		for _, rel := range relsDoc.Root().SelectElements("Relationship") {
			targets[rel.SelectAttrValue("Id", "")] = rel.SelectAttrValue("Target", "")
		}
	}
	index := 0
	var walk func(*etree.Element) error
	walk = func(elem *etree.Element) error {
		if elem.Tag == "Shape" && strings.EqualFold(elem.SelectAttrValue("Type", ""), "Foreign") {
			if fd := elem.SelectElement("ForeignData"); fd != nil {
				if rel := fd.SelectElement("Rel"); rel != nil {
					target := targets[rel.SelectAttrValue("id", "")]
					media := filepath.ToSlash(filepath.Join("visio", "media", filepath.Base(target)))
					if data, ok := stencil.ZipFileContents[media]; ok && strings.EqualFold(filepath.Ext(media), ".svg") {
						index++
						name := fmt.Sprintf("layer-%02d-shape-%s.svg", index, elem.SelectAttrValue("ID", "unknown"))
						if err := os.WriteFile(filepath.Join(outputDir, name), data, 0o644); err != nil {
							return err
						}
					}
				}
			}
		}
		for _, child := range elem.ChildElements() {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}
