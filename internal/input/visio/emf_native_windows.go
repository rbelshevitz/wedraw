//go:build windows && (amd64 || arm64)

package visio

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	"github.com/deploymenttheory/go-bindings-win32/bindings/win32/graphics/gdiplus"
)

// nativeMetafileToSVG lets Windows GDI+ replay EMF and EMF+ records, then
// wraps the resulting PNG in SVG so the existing draw.io media path remains
// unchanged. This is deliberately Windows-only: GDI+ is the fidelity oracle
// for vendor artwork that is not representable by ordinary Visio Geometry.
func nativeMetafileToSVG(data []byte) ([]byte, error) {
	in, err := os.CreateTemp("", "wedraw-*.emf")
	if err != nil {
		return nil, err
	}
	inPath := in.Name()
	defer os.Remove(inPath)
	if _, err := in.Write(data); err != nil {
		in.Close()
		return nil, err
	}
	if err := in.Close(); err != nil {
		return nil, err
	}

	out, err := os.CreateTemp("", "wedraw-*.png")
	if err != nil {
		return nil, err
	}
	outPath := out.Name()
	if err := out.Close(); err != nil {
		os.Remove(outPath)
		return nil, err
	}
	defer os.Remove(outPath)

	var token uintptr
	startup := gdiplus.GdiplusStartupInput{GdiplusVersion: 1}
	if status := gdiplus.GdiplusStartup(&token, &startup, nil); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ startup failed: %s", status)
	}
	defer gdiplus.GdiplusShutdown(token)

	var image *gdiplus.GpImage
	if status := gdiplus.GdipLoadImageFromFile(inPath, &image); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not load EMF: %s", status)
	}
	defer gdiplus.GdipDisposeImage(image)

	// CLSID_PngEncoder: {557cf406-1a04-11d3-9a73-0000f81ef32e}.
	pngEncoder := win32.GUID{Data1: 0x557cf406, Data2: 0x1a04, Data3: 0x11d3, Data4: [8]byte{0x9a, 0x73, 0x00, 0x00, 0xf8, 0x1e, 0xf3, 0x2e}}
	var sourceWidth, sourceHeight uint32
	if status := gdiplus.GdipGetImageWidth(image, &sourceWidth); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not read EMF width: %s", status)
	}
	if status := gdiplus.GdipGetImageHeight(image, &sourceHeight); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not read EMF height: %s", status)
	}
	if sourceWidth == 0 || sourceHeight == 0 {
		return nil, errors.New("GDI+ reported an empty EMF")
	}

	// Vendor EMF files can advertise enormous logical frames. Render to a
	// bounded bitmap instead of asking GDI+ to allocate that frame verbatim.
	// Library entries are scaled by draw.io after import. Match raster quality
	// to the amount of source artwork: small icons stay compact, while large
	// vendor illustrations retain more detail.
	width, height := int32(sourceWidth), int32(sourceHeight)
	// EMF files may advertise a tiny nominal bitmap (for example 25x8) even
	// though their records are vector instructions. Always render to the
	// requested long edge, scaling down large frames and scaling up tiny ones.
	target := float64(metafileResolution)
	scale := target / math.Max(float64(width), float64(height))
	if scale <= 0 {
		return nil, errors.New("invalid EMF render scale")
	}
	width = int32(math.Max(1, math.Round(float64(width)*scale)))
	height = int32(math.Max(1, math.Round(float64(height)*scale)))

	var bitmap *gdiplus.GpBitmap
	const pixelFormat32bppARGB = 0x26200a
	if status := gdiplus.GdipCreateBitmapFromScan0(width, height, 0, pixelFormat32bppARGB, nil, &bitmap); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not allocate bounded bitmap: %s", status)
	}
	defer gdiplus.GdipDisposeImage((*gdiplus.GpImage)(bitmap))
	var graphics *gdiplus.GpGraphics
	if status := gdiplus.GdipGetImageGraphicsContext((*gdiplus.GpImage)(bitmap), &graphics); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not create bitmap graphics: %s", status)
	}
	defer gdiplus.GdipDeleteGraphics(graphics)
	if status := gdiplus.GdipGraphicsClear(graphics, 0x00ffffff); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not clear bitmap: %s", status)
	}
	if status := gdiplus.GdipDrawImageRectI(graphics, image, 0, 0, width, height); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not draw EMF: %s", status)
	}
	if status := gdiplus.GdipSaveImageToFile((*gdiplus.GpImage)(bitmap), outPath, &pngEncoder, nil); status != gdiplus.Ok {
		return nil, fmt.Errorf("GDI+ could not save PNG: %s", status)
	}
	png, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	if len(png) == 0 {
		return nil, errors.New("GDI+ produced an empty PNG")
	}

	var encoded bytes.Buffer
	encoded.WriteString("data:image/png;base64,")
	encoder := base64.NewEncoder(base64.StdEncoding, &encoded)
	if _, err := encoder.Write(png); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d"><image href="%s" x="0" y="0" width="%d" height="%d" preserveAspectRatio="none"/></svg>`, width, height, width, height, encoded.String(), width, height)), nil
}
