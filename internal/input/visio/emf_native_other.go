//go:build !windows

package visio

func nativeMetafileToSVG([]byte) ([]byte, error) {
	return nil, errNativeMetafileUnavailable
}
