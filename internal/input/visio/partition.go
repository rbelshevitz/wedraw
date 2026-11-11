package visio

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/beevik/etree"
)

// SplitStencil creates small, self-contained VSSX parts containing at most
// chunkSize masters each. A part keeps the original master XML bytes intact;
// only masters.xml is filtered. This is intended for process-isolated batch
// conversion when a complete stencil is too large for comfortable RAM use.
func SplitStencil(input, outputDir string, chunkSize int) ([]string, error) {
	if chunkSize < 1 {
		return nil, fmt.Errorf("chunk size must be positive")
	}
	reader, err := zip.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("open stencil zip: %w", err)
	}
	defer reader.Close()

	entries := make(map[string]*zip.File, len(reader.File))
	for _, entry := range reader.File {
		entries[entry.Name] = entry
	}
	mastersEntry := entries["visio/masters/masters.xml"]
	if mastersEntry == nil {
		return nil, fmt.Errorf("stencil has no visio/masters/masters.xml")
	}
	mastersBytes, err := readZipEntry(mastersEntry)
	if err != nil {
		return nil, err
	}
	document := etree.NewDocument()
	if err := document.ReadFromBytes(mastersBytes); err != nil {
		return nil, fmt.Errorf("parse masters.xml: %w", err)
	}
	root := document.Root()
	if root == nil {
		return nil, fmt.Errorf("masters.xml has no root element")
	}
	masters := root.SelectElements("Master")
	if len(masters) == 0 {
		return nil, fmt.Errorf("masters.xml has no masters")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create stencil parts directory: %w", err)
	}

	paths := make([]string, 0, (len(masters)+chunkSize-1)/chunkSize)
	for start, part := 0, 1; start < len(masters); start, part = start+chunkSize, part+1 {
		partRoot := root.Copy()
		for _, master := range partRoot.SelectElements("Master") {
			_ = partRoot.RemoveChild(master)
		}
		for _, master := range masters[start:min(start+chunkSize, len(masters))] {
			partRoot.AddChild(master.Copy())
		}
		partDocument := etree.NewDocument()
		partDocument.SetRoot(partRoot)
		filteredMasters, err := partDocument.WriteToBytes()
		if err != nil {
			return nil, fmt.Errorf("serialize stencil part %d: %w", part, err)
		}

		path := filepath.Join(outputDir, "part-"+strconv.Itoa(part)+".vssx")
		if err := writeStencilPart(path, entries, filteredMasters, partRoot); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func writeStencilPart(path string, entries map[string]*zip.File, mastersXML []byte, root *etree.Element) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create stencil part %s: %w", path, err)
	}
	archive := zip.NewWriter(file)
	archiveClosed := false
	complete := false
	defer func() {
		if !archiveClosed {
			_ = archive.Close()
		}
		_ = file.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()

	writeError := func(operation string, err error) error {
		return fmt.Errorf("%s in %s: %w", operation, path, err)
	}

	if contentTypes := entries["[Content_Types].xml"]; contentTypes != nil {
		if err := copyZipEntry(archive, contentTypes); err != nil {
			return err
		}
	}
	masterWriter, err := archive.Create("visio/masters/masters.xml")
	if err != nil {
		return writeError("create masters.xml", err)
	}
	if _, err := masterWriter.Write(mastersXML); err != nil {
		return writeError("write masters.xml", err)
	}

	for _, master := range root.SelectElements("Master") {
		id := master.SelectAttrValue("ID", "")
		entry := entries["visio/masters/master"+id+".xml"]
		if entry == nil {
			return writeError("find master "+id, fmt.Errorf("missing XML"))
		}
		if err := copyZipEntry(archive, entry); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return writeError("close archive", err)
	}
	archiveClosed = true
	if err := file.Close(); err != nil {
		return writeError("close file", err)
	}
	complete = true
	return nil
}

func copyZipEntry(destination *zip.Writer, source *zip.File) error {
	writer, err := destination.Create(source.Name)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", source.Name, err)
	}
	reader, err := source.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", source.Name, err)
	}
	defer reader.Close()
	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("copy zip entry %s: %w", source.Name, err)
	}
	return nil
}

func readZipEntry(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open zip entry %s: %w", entry.Name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read zip entry %s: %w", entry.Name, err)
	}
	return data, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
