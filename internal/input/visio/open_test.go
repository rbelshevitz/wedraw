package visio

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"testing"
)

func TestRepairMasterArchiveUsesRelationshipTarget(t *testing.T) {
	source := t.TempDir() + "\\shuffled.vssx"
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	parts := map[string]string{
		"visio/masters/masters.xml":            `<Masters xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><Master ID="14" NameU="Foo"><Rel r:id="rId6"/></Master></Masters>`,
		"visio/masters/_rels/masters.xml.rels": `<Relationships><Relationship Id="rId6" Target="master6.xml"/></Relationships>`,
		"visio/masters/master6.xml":            `<PageContents><Shape NameU="Foo"/></PageContents>`,
		"visio/masters/master14.xml":           `<PageContents><Shape NameU="Other"/></PageContents>`,
	}
	for name, data := range parts {
		entry, createErr := zw.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := io.WriteString(entry, data); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	repaired, cleanup, changed, err := repairMasterArchive(source)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !changed {
		t.Fatal("expected shuffled relationship target to be repaired")
	}

	r, err := zip.OpenReader(repaired)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var master14 []byte
	for _, entry := range r.File {
		if entry.Name != "visio/masters/master14.xml" {
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		master14, err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Contains(master14, []byte(`NameU="Foo"`)) {
		t.Fatalf("master14.xml was not materialized from relationship target: %s", master14)
	}
}
