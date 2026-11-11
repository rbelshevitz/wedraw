# wedraw

A CLI tool that makes Visio VSSX stencils usable as diagrams.net / draw.io libraries — without Microsoft Visio.

Have a useful Visio stencil but no Windows computer or Microsoft Visio at hand? Download wedraw, point it at the `.vssx` file, and import the result into draw.io.

![An experienced technical drafter working with an editable network diagram](assets/wedraw-drafter.png)

## Why this tool exists

Many Cisco and other vendor stencil collections were designed for Visio. Rebuilding hundreds of network symbols by hand is not practical, while a plain SVG export can lose labels, colors, connection points, or editable parts. wedraw reads `.vssx` directly and creates an importable draw.io library without requiring Visio or Office.

The generated library keeps:

- the original vector appearance, including complex curves;
- shape names and visible labels;
- basic fills, strokes, and colors;
- connection points where the source stencil provides them.

## Quick start

### Download and run

When a release is published, download the binary for your system from the [Releases](https://github.com/rbelshevitz/wedraw/releases) page. The planned `v0.1` release will include:

- `wedraw-windows-amd64.exe` for 64-bit Windows;
- `wedraw-linux-amd64` for 64-bit Linux.

Then run:

```text
wedraw-windows-amd64.exe Cisco.vssx -o Cisco.drawio-library.xml
```

or on Linux:

```text
./wedraw-linux-amd64 Cisco.vssx -o Cisco.drawio-library.xml
```

No Visio or Office installation is needed.

### Build from source

```powershell
go build -o wedraw.exe ./cmd/wedraw
```

Then use the newly built executable in the same way as the downloaded binary.

If `-o` is omitted, the library is written next to the input file:

```text
input.drawio-library.xml
```

In draw.io, choose **File → Import Library**, select the generated file, and drag a shape onto the canvas.

To inspect a stencil before creating its library:

```powershell
wedraw inspect input.vssx
```

It reports the number of shapes, names, and connection points.

## Choose an editing mode

The default output is the safe, memory-conscious mode:

```powershell
wedraw input.vssx -o output.drawio-library.xml --mode faithful
```

Each shape keeps its complete vector artwork in one layer, while its visible text label remains editable. Use this mode when visual fidelity matters most or when working with a large collection.

For more editing, use:

```powershell
wedraw input.vssx -o output.drawio-library.xml --mode ungroup
```

This mode exposes safe paths and text as separate native draw.io cells. One **Arrange → Ungroup** opens the editable elements. Not every Visio feature has a safe native equivalent: compound contours, images, gradients, markers, shadows, rotated geometry, and unsupported curves remain as a complete SVG fallback so graphics are not silently simplified.

The original fill and line colors are kept. A line color can be changed only on an element that had a stroke in the source stencil; fill-only elements deliberately remain without an artificial outline.

## Options

- `--mode faithful` is the default and keeps each figure's artwork together.
- `--mode ungroup` exposes safe paths and text as separate cells for editing.
- `--label-gap 15` controls the space between the artwork and its label.

Lower `--label-gap` for compact symbols; raise it when a label needs more separation:

```powershell
wedraw input.vssx -o output.drawio-library.xml --mode ungroup --label-gap 8
```

`ungroup` needs more RAM because it builds many editable objects. On a 486-shape Cisco stencil, a test run peaked at about **552 MB of process memory**, compared with about **471 MB** in `faithful` mode. Use `faithful` on machines with limited RAM or large stencil collections.

## Development

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache'
$env:GOFLAGS = '-trimpath'
go test ./...
```

For the large local Cisco fixture, run `pwsh -File scripts/memory-test.ps1` to measure memory use.

## Third-party licenses

License notices for the Go libraries used by wedraw are collected in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

wedraw itself is released under the [MIT License](LICENSE).
