package visio

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/beevik/etree"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// OpenStencil repairs vendor stencils whose masters.xml index names do not
// match the numeric master filenames. Some Lenovo files contain such a
// shuffle; vsdx-go otherwise pairs a correct name with the wrong artwork.
func OpenStencil(filename string) (*vsdx.Stencil, func(), error) {
	temp, cleanup, repaired, err := repairMasterArchive(filename)
	if err != nil {
		return nil, func() {}, err
	}
	path := filename
	if repaired {
		path = temp
	}
	stencil, err := vsdx.OpenStencil(path)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return stencil, cleanup, nil
}

type archiveEntry struct {
	name string
	data []byte
}

func repairMasterArchive(filename string) (string, func(), bool, error) {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return "", func() {}, false, err
	}
	defer r.Close()
	entries := make([]archiveEntry, 0, len(r.File))
	for _, file := range r.File {
		reader, err := file.Open()
		if err != nil {
			return "", func() {}, false, err
		}
		data, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil {
			return "", func() {}, false, readErr
		}
		entries = append(entries, archiveEntry{name: file.Name, data: data})
	}
	var mastersData []byte
	for _, entry := range entries {
		if entry.name == "visio/masters/masters.xml" {
			mastersData = entry.data
			break
		}
	}
	if len(mastersData) == 0 {
		return "", func() {}, false, fmt.Errorf("stencil has no masters.xml")
	}
	index := etree.NewDocument()
	if err := index.ReadFromBytes(mastersData); err != nil {
		return "", func() {}, false, err
	}
	type masterIndexEntry struct{ id, name string }
	var indexed []masterIndexEntry
	for _, elem := range index.Root().SelectElements("Master") {
		name := elem.SelectAttrValue("NameU", "")
		if name == "" {
			name = elem.SelectAttrValue("Name", "")
		}
		indexed = append(indexed, masterIndexEntry{id: elem.SelectAttrValue("ID", ""), name: name})
	}
	type masterFile struct {
		xml, rels      []byte
		key            string
		rootName, text string
	}
	files := make(map[string]masterFile)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.name, "visio/masters/master") || !strings.HasSuffix(entry.name, ".xml") || strings.Contains(entry.name, "_rels/") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(entry.name), "master"), ".xml")
		rootName, rootText := masterMetadata(entry.data)
		files[id] = masterFile{xml: entry.data, key: entry.name, rootName: rootName, text: rootText}
	}
	for _, entry := range entries {
		if !strings.Contains(entry.name, "visio/masters/_rels/master") || !strings.HasSuffix(entry.name, ".xml.rels") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(entry.name), "master"), ".xml.rels")
		file := files[id]
		file.rels = entry.data
		files[id] = file
	}
	assign := make(map[string]masterFile)
	used := make(map[string]bool)
	for _, item := range indexed {
		bestID, bestScore := "", 0
		for id, file := range files {
			if used[id] {
				continue
			}
			score := masterMatchScore(item.name, file.rootName, file.text)
			if score > bestScore {
				bestID, bestScore = id, score
			}
		}
		if bestScore >= 100 {
			assign[item.id] = files[bestID]
			used[bestID] = true
		}
	}
	if len(assign) == 0 {
		return "", func() {}, false, nil
	}
	changed := false
	for id, file := range assign {
		if file.key != "visio/masters/master"+id+".xml" {
			changed = true
			break
		}
	}
	if !changed {
		return "", func() {}, false, nil
	}
	tempFile, err := os.CreateTemp("", "wedraw-repaired-*.vssx")
	if err != nil {
		return "", func() {}, false, err
	}
	tempPath := tempFile.Name()
	cleanup := func() {
		_ = os.Remove(tempPath)
	}
	zw := zip.NewWriter(tempFile)
	replacement := make(map[string][]byte)
	replacementRels := make(map[string][]byte)
	for id, file := range assign {
		replacement["visio/masters/master"+id+".xml"] = file.xml
		if len(file.rels) > 0 {
			replacementRels["visio/masters/_rels/master"+id+".xml.rels"] = file.rels
		}
	}
	written := make(map[string]bool)
	for _, entry := range entries {
		data := entry.data
		if replacementData, ok := replacement[entry.name]; ok {
			data = replacementData
		}
		if replacementData, ok := replacementRels[entry.name]; ok {
			data = replacementData
		}
		w, createErr := zw.Create(entry.name)
		if createErr != nil {
			cleanup()
			return "", func() {}, false, createErr
		}
		if _, writeErr := io.Copy(w, bytes.NewReader(data)); writeErr != nil {
			cleanup()
			return "", func() {}, false, writeErr
		}
		written[entry.name] = true
	}
	// A shuffled vendor stencil may have no .rels part under the target
	// master ID. Create the repaired relationship part instead of silently
	// leaving the ForeignData links unresolved.
	for name, data := range replacementRels {
		if written[name] {
			continue
		}
		w, createErr := zw.Create(name)
		if createErr != nil {
			cleanup()
			return "", func() {}, false, createErr
		}
		if _, writeErr := io.Copy(w, bytes.NewReader(data)); writeErr != nil {
			cleanup()
			return "", func() {}, false, writeErr
		}
	}
	if err := zw.Close(); err != nil {
		cleanup()
		return "", func() {}, false, err
	}
	if err := tempFile.Close(); err != nil {
		cleanup()
		return "", func() {}, false, err
	}
	return tempPath, cleanup, true, nil
}

func masterMetadata(data []byte) (string, string) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil || doc.Root() == nil {
		return "", ""
	}
	shape := doc.Root().FindElement(".//Shape")
	if shape == nil {
		return "", ""
	}
	rootName := shape.SelectAttrValue("NameU", "")
	rootText := ""
	if text := shape.SelectElement("Text"); text != nil {
		rootText = strings.TrimSpace(text.Text())
	}
	return rootName, rootText
}

func masterMatchScore(name, rootName, rootText string) int {
	want := normalizeKey(name)
	score := 0
	if want != "" && strings.Contains(normalizeKey(rootName), want) {
		score = 1000
	}
	if score == 0 {
		wantTokens := strings.Fields(want)
		rootTokens := strings.Fields(normalizeKey(rootName))
		textTokens := strings.Fields(normalizeKey(rootText))
		matched := 0
		for _, token := range wantTokens {
			if len(token) < 2 {
				continue
			}
			if containsToken(rootTokens, token) || containsToken(textTokens, token) {
				matched++
			}
		}
		// Prefer candidates containing every meaningful requested token,
		// then prefer the one with fewer unrelated tokens. This matters for
		// Lenovo names such as "C14 Inlet PSU" vs "C14 Inlet CRPS PSU".
		extra := 0
		for _, token := range rootTokens {
			if len(token) >= 2 && !containsToken(wantTokens, token) && token != "lenovo" && token != "thinksystem" {
				extra++
			}
		}
		score = matched*100 - extra*50
		if matched == len(wantTokens) {
			score += 500
		}
	}
	return score
}

func containsToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}

func normalizeKey(value string) string {
	value = strings.ToLower(value)
	value = strings.TrimPrefix(value, "lenovo_")
	value = nonWord.ReplaceAllString(value, " ")
	value = strings.Join(strings.Fields(value), " ")
	// Lenovo's masters index uses "Vx" for the V2 family in a few
	// entries, while the corresponding master XML says "V2".
	value = strings.ReplaceAll(value, " vx ", " v2 ")
	// The index calls the generic 7mm component a disk; the master XML
	// uses the vendor's "Drive" wording.
	value = strings.ReplaceAll(value, " disk", " drive")
	return value
}
