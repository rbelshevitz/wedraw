package visio

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wijnberg-net/vsdx-go/vsdx"
)

func TestSplitStencilKeepsMasterOrderAndContent(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.vssx")
	writeTestStencil(t, source, 5)
	parts, err := SplitStencil(source, t.TempDir(), 2)
	if err != nil {
		t.Fatalf("split stencil: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want 3", len(parts))
	}

	wantCounts := []int{2, 2, 1}
	for i, part := range parts {
		stencil, err := vsdx.OpenStencil(part)
		if err != nil {
			t.Fatalf("open part %d: %v", i+1, err)
		}
		if got := len(stencil.GetMasterNames()); got != wantCounts[i] {
			t.Errorf("part %d has %d masters, want %d", i+1, got, wantCounts[i])
		}
	}
}

func writeTestStencil(t *testing.T, path string, count int) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	masters := `<Masters xmlns="http://schemas.microsoft.com/office/visio/2012/main">`
	for i := 1; i <= count; i++ {
		masters += fmt.Sprintf(`<Master ID="%d" NameU="Shape %d" Name="Shape %d"/>`, i, i, i)
	}
	masters += `</Masters>`
	writeZipText(t, archive, "visio/masters/masters.xml", masters)
	for i := 1; i <= count; i++ {
		writeZipText(t, archive, fmt.Sprintf("visio/masters/master%d.xml", i),
			`<MasterContents xmlns="http://schemas.microsoft.com/office/visio/2012/main"><Shapes/></MasterContents>`)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZipText(t *testing.T, archive *zip.Writer, name, value string) {
	t.Helper()
	entry, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(value)); err != nil {
		t.Fatal(err)
	}
}
