# Format Notes

This note explains why some Visio stencils convert cleanly and others need a more careful rendering path.

## Visio VSSX

`.vssx` is an Office Open XML package — in practice, a ZIP archive containing XML parts and optional media. The important parts are usually:

- `visio/masters/masters.xml` — the list of stencil masters and their names;
- `visio/masters/masterN.xml` — the shape tree for each master;
- relationship files — links from a master to external media;
- `visio/media/` — EMF, WMF, SVG, or bitmap artwork.

A master is more than one picture. It may contain groups, nested coordinate systems, text, connection points, and several visual layers.

## Two common kinds of artwork

### Native Geometry

Native Visio `Shape` and `Geometry` elements describe paths directly. They carry fills, line colors, text, and connection points. Cisco collections commonly use this structure, so `wedraw` can preserve them as scalable draw.io geometry.

### Foreign artwork

`Foreign` shapes reference EMF/WMF data through relationship files. Detailed server and networking stencils often combine several Foreign layers with native frames, fills, panels, or ports.

Important consequences:

- the root group can have a misleading near-zero height;
- the real canvas must be calculated from transformed child shapes;
- Foreign and native layers must share one coordinate system and source order;
- dropping the native or Foreign part produces missing backgrounds, isolated details, or plain rectangles.

On Windows, EMF is replayed through GDI+ into a bounded PNG embedded inside SVG. The `--resolution` option controls its long edge. On Linux, the converter uses the portable Go EMF fallback instead; native Geometry remains fully supported, but difficult EMF+/raster records may render with lower fidelity. The result is sharp, but it is not infinitely scalable like the original EMF.

For a simple native-geometry stencil, the platform usually does not matter. For an EMF-heavy vendor stencil, Windows is the recommended conversion environment when matching the original Visio appearance is important.

## Inspecting an input stencil

Before conversion, `wedraw inspect input.vssx` reports the masters found in the package, their names, and connection-point counts. This is useful for checking a vendor stencil without opening Visio. It does not render the artwork; use a generated draw.io library to review the final appearance.

## draw.io library output

The result is a custom draw.io library:

```xml
<mxlibrary>[JSON entries]</mxlibrary>
```

Each entry has a title, tags, dimensions, and an `mxGraphModel`. Complex figures are stored as a grouped SVG image with a separate editable text cell. Connection points are written to the group style. The title is also written as searchable metadata so draw.io can find the figure by name.

## Conversion policy

- Simple native geometry is converted to editable draw.io cells where safe.
- Complex layered artwork defaults to one faithful grouped figure.
- `ungroup` is deliberately conservative: unsupported or risky content remains as SVG instead of being replaced with guessed primitives.

## References

- [Microsoft Visio file format](https://learn.microsoft.com/en-us/office/client-developer/visio/introduction-to-the-visio-file-formatvsdx)
- [draw.io custom library format](https://www.drawio.com/docs/reference/format-custom-shape-library/)
- [draw.io shape search](https://www.drawio.com/docs/manual/shapes/shape-search/)

## Diagnostic script

The repository also contains `scripts/scan-stencil.ps1`. It is a developer aid for inspecting a `.vssx` package without Visio. It reports the number of masters, groups, native Geometry and Foreign shapes, nesting depth, degenerate root sizes, relationship files, and media types such as EMF, SVG, and PNG.

The script does not convert a stencil and is not required for normal use of `wedraw`.

The repository also contains `scripts/memory-test.ps1` for Windows-side memory checks. It builds the CLI, runs a selected mode against a local `.vssx` fixture, records peak Working Set and Private Memory, and fails when the configured budget is exceeded. Its default fixture is the local Cisco test file; pass `-Fixture`, `-Mode faithful` or `-Mode ungroup`, and `-MaxPrivateMB` to adjust the run.

Both scripts are development tools. They are not needed to use a release binary.
