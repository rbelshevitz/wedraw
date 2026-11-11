# wedraw

Go CLI for converting Visio VSSX stencils into importable diagrams.net / draw.io libraries — without Microsoft Visio.

Have a useful Visio stencil but no Windows computer or Microsoft Visio at hand? Download wedraw, point it at the `.vssx` file, and import the generated library into draw.io.

![An experienced technical drafter working with an editable network diagram](assets/wedraw-drafter.png)

## Why this tool exists

Many Cisco and other vendor stencil collections were designed for Visio. People who use draw.io may have the stencil files but no Visio or Microsoft Office to open them. Rebuilding hundreds of network symbols by hand is not practical, and a plain SVG export can lose labels, colors, connection points, or editable parts.

`wedraw` reads `.vssx` directly and creates a library that draw.io can import. Visio and Microsoft Office are not required.

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
go run ./cmd/wedraw input.vssx -o output.drawio-library.xml
```

The explicit form `wedraw convert input.vssx ...` is also supported, but is not required.

If `-o` is omitted, wedraw writes the library next to the input file:

```text
input.drawio-library.xml
```

In draw.io, choose **File → Import Library**, select the generated file, and drag a shape onto the canvas.

To inspect a stencil before converting it:

```powershell
go run ./cmd/wedraw inspect input.vssx
```

The command reports the number of shapes, names, and connection points.

## Choose an editing mode

The default output is the safe, memory-conscious mode:

```powershell
go run ./cmd/wedraw convert input.vssx -o output.drawio-library.xml --mode faithful
```

Each shape keeps its complete vector artwork in one layer, while its visible text label remains editable. Use this mode when visual fidelity matters most or when converting a large collection.

For more editing, use:

```powershell
go run ./cmd/wedraw convert input.vssx -o output.drawio-library.xml --mode ungroup
```

This mode exposes safe paths and text as separate native draw.io cells. One **Arrange → Ungroup** opens the editable elements. Not every Visio feature has a safe native equivalent: compound contours, images, gradients, markers, shadows, rotated geometry, and unsupported curves remain as a complete SVG fallback so graphics are not silently simplified.

The original fill and line colors are kept. A line color can be changed only on an element that had a stroke in the source stencil; fill-only elements deliberately remain without an artificial outline.

## Command options

`--mode faithful` is the default. Use `--mode ungroup` when you need to recolor or edit individual safe elements.

`--label-gap 15` controls the space between the artwork and its editable label. Lower it for compact symbols; raise it when a label needs more separation:

```powershell
go run ./cmd/wedraw convert input.vssx -o output.drawio-library.xml --mode ungroup --label-gap 8
```

`ungroup` needs more RAM because it builds many editable objects. On a 486-shape Cisco stencil, a test conversion peaked at about **552 MB of process memory**, compared with about **471 MB** in `faithful` mode. Use `faithful` on machines with limited RAM or large stencil collections.

## Development

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache'
$env:GOFLAGS = '-trimpath'
go test ./...
```

For the large local Cisco fixture, run `pwsh -File scripts/memory-test.ps1` to measure conversion memory use.

## Third-party licenses

License notices for the Go libraries used by wedraw are collected in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

wedraw itself is released under the [MIT License](LICENSE).
