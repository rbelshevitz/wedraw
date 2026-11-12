# wedraw

![An experienced technical drafter working with an editable network diagram](assets/wedraw-drafter.png)

[![CI & Malware scan](https://img.shields.io/github/actions/workflow/status/rbelshevitz/wedraw/ci.yml?branch=main&label=CI%20%26%20Malware%20scan)](https://github.com/rbelshevitz/wedraw/actions/workflows/ci.yml) [![Go version](https://img.shields.io/github/go-mod/go-version/rbelshevitz/wedraw?label=Go)](https://go.dev/)

A Go CLI that converts Microsoft Visio `.vssx` stencils into libraries that diagrams.net / draw.io can import.

`wedraw` is an independent project. It is not affiliated with Microsoft, draw.io Ltd., or draw.io AG.

## Why wedraw?

Many useful Cisco, HPE, and other vendor stencils are made for Visio. Visio is not available to everyone, and draw.io does not provide a general VSSX import workflow. `wedraw` reads the stencil directly and creates a reusable draw.io library.

It preserves the important parts of a stencil:

- the visible artwork and proportions;
- names and editable labels;
- basic fills, strokes, and colors;
- connection points supplied by the source.

## Quick start

The `v0.2` release publishes Windows and Linux AMD64 executables. Download the appropriate file from [Releases](https://github.com/rbelshevitz/wedraw/releases), then run:

```powershell
wedraw-windows-amd64.exe Cisco.vssx -o Cisco.drawio-library.xml
```

On Linux, build from source:

```bash
go build -o wedraw ./cmd/wedraw
./wedraw Cisco.vssx -o Cisco.drawio-library.xml
```

Open the draw.io app, choose **File → Import Library**, select the generated file, and add the imported library to the shape panel. Then drag a shape onto the canvas.

If `-o` is omitted, the output is written next to the input file.

## Options

```text
wedraw input.vssx [options]
```

- `--mode faithful` — default; keeps each figure together and preserves difficult artwork.
- `--mode ungroup` — exposes safe native paths and text as separate cells. Use mainly with simple geometry stencils.
- `--label-gap 6` — distance between artwork and its label, in pixels.
- `--resolution 2048` — target long edge for Windows EMF rendering. Use `3072` or `4096` for sharper zoom; this uses more RAM and disk space. Windows only.
- `-v` — print one progress message per shape.
- `-h` — show help. `-V` — show the version.

For complex vendor artwork, use the default `faithful` mode. `ungroup` cannot safely split every layered EMF/Foreign figure without risking lost layers or incorrect placement.

> **Platform note:** Native geometry works on both platforms. Windows uses GDI+ for difficult EMF artwork. Linux uses a portable Go fallback, so complex EMF details may differ; for the best match to Visio, convert EMF-heavy stencils on Windows.

VSSX, EMF, and draw.io libraries have several format-specific details. If you are interested in the technical side, read [docs/FORMATS.md](docs/FORMATS.md).

## License

wedraw is released under the [MIT License](LICENSE). Third-party notices are in [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md).
