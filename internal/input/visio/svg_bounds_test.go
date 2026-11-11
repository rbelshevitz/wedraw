package visio

import (
	"strings"
	"testing"
)

func TestExpandSVGForTextKeepsLabelInsideViewBox(t *testing.T) {
	source := []byte(`<svg viewBox="-6.23 -6.23 192.46 192.46"><path d="M0 0"/><text x="90" y="232.62" font-size="25.4">Access Point</text></svg>`)
	updated, width, height, err := ExpandSVGForText(source, 180, 180)
	if err != nil {
		t.Fatal(err)
	}
	if width != 180 || height <= 180 {
		t.Fatalf("dimensions = %.1fx%.1f, want width 180 and expanded height", width, height)
	}
	if !strings.Contains(string(updated), `viewBox="-6.2300 -6.2300 192.4600`) {
		t.Fatalf("updated SVG did not preserve viewBox origin and width: %s", updated)
	}
	if strings.Contains(string(updated), `viewBox="-6.23 -6.23 192.46 192.46"`) {
		t.Fatal("viewBox height was not expanded")
	}
}

func TestExpandSVGForTextLeavesContainedTextUnchanged(t *testing.T) {
	source := []byte(`<svg viewBox="0 0 100 100"><text x="50" y="50" font-size="10">Inside</text></svg>`)
	updated, width, height, err := ExpandSVGForText(source, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(source) || width != 100 || height != 100 {
		t.Fatalf("contained text changed SVG or size: %q %.1fx%.1f", updated, width, height)
	}
}

func TestShiftSVGTextMovesTextAndMultilineSpans(t *testing.T) {
	source := []byte(`<svg><text x="102.7"><tspan x="102.7">Access</tspan><tspan x="102.7" dy="1.2em">Point</tspan></text></svg>`)
	updated, err := ShiftSVGText(source, 12.7)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if strings.Count(text, `x="90.0000"`) != 3 {
		t.Fatalf("text positions were not shifted consistently: %s", text)
	}
}
