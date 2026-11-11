package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/wijnberg-net/vsdx-go/vsdx"
)

// TestStencilMemoryBudget is an opt-in integration test for large real-world
// stencils. It deliberately does not run in the normal unit-test suite because
// it needs a local fixture and takes several seconds. Heap measurements are
// reported after GC so the test tracks retained model/rendering memory rather
// than short-lived allocations.
func TestStencilMemoryBudget(t *testing.T) {
	if os.Getenv("WEDRAW_MEMORY_TEST") != "1" {
		t.Skip("set WEDRAW_MEMORY_TEST=1 to run the large-stencil memory test")
	}

	fixture := os.Getenv("WEDRAW_MEMORY_FIXTURE")
	if fixture == "" {
		fixture = `D:\Temp\VectaDrawFixtures\Cisco\metric\Stencil-Metric\Cisco_All-Icons_V-1_m.vssx`
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("memory fixture is unavailable: %s", fixture)
	}

	budgetMB := 384
	if raw := os.Getenv("WEDRAW_MEMORY_BUDGET_MB"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 64 {
			t.Fatalf("invalid WEDRAW_MEMORY_BUDGET_MB=%q", raw)
		}
		budgetMB = parsed
	}

	stencil, err := vsdx.OpenStencil(fixture)
	if err != nil {
		t.Fatalf("open stencil: %v", err)
	}
	names := stencil.GetMasterNames()
	if len(names) == 0 {
		t.Fatal("fixture contains no masters")
	}

	var peakHeap uint64
	for i, name := range names {
		master := stencil.GetMaster(name)
		if master == nil {
			continue
		}
		if _, err := vsdx.MasterToSVG(master, vsdx.WithSize(240, 180), vsdx.WithPrecision(2)); err != nil {
			t.Fatalf("render %q: %v", name, err)
		}
		if i%8 == 0 || i == len(names)-1 {
			runtime.GC()
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			if stats.HeapInuse > peakHeap {
				peakHeap = stats.HeapInuse
			}
		}
	}

	peakMB := float64(peakHeap) / (1024 * 1024)
	t.Logf("fixture=%s masters=%d retained_heap_peak=%.1f MB budget=%d MB", filepath.Base(fixture), len(names), peakMB, budgetMB)
	if peakHeap > uint64(budgetMB)*1024*1024 {
		t.Fatalf("retained heap peak %.1f MB exceeds budget %d MB", peakMB, budgetMB)
	}
}
