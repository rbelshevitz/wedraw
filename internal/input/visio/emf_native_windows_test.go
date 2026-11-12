//go:build windows && (amd64 || arm64)

package visio

import (
	"os"
	"testing"
)

func TestNativeMetafileOneLenovoFixture(t *testing.T) {
	data, err := os.ReadFile(".tmp-lenovo-image257.emf")
	if err != nil {
		t.Skip("Lenovo fixture is not present")
	}
	svg, err := nativeMetafileToSVG(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(svg) < 1000 {
		t.Fatalf("native render is unexpectedly small: %d bytes", len(svg))
	}
}
