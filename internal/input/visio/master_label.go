package visio

import (
	"strings"

	"github.com/wijnberg-net/vsdx-go/vsdx"
)

// MasterLabel returns the master name used by the stencil index. In vendor
// files, the root Shape.Text may actually be inherited text from a nested
// component (for example a GPU or adapter), so it is not a reliable master
// title and must not replace the indexed master name.
func MasterLabel(master *vsdx.Page) string {
	if master == nil {
		return ""
	}
	return strings.TrimSpace(master.Name())
}
