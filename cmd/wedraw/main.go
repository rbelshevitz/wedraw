package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/wedraw-project/wedraw/internal/input/visio"
	"github.com/wedraw-project/wedraw/internal/library"
	"github.com/wijnberg-net/vsdx-go/vsdx"
)

const version = "0.3"
const author = "Roman Belshevitz"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "-V", "--version":
		printVersion()
		return
	case "inspect":
		err = inspect(os.Args[2:])
	case "debug":
		err = debug(os.Args[2:])
	case "convert":
		err = convert(os.Args[2:])
	case "help", "--help", "-h":
		usage()
		return
	default:
		if !strings.HasPrefix(os.Args[1], "-") {
			err = convert(os.Args[1:])
			break
		}
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("wedraw converts Visio stencils into draw.io libraries.")
	fmt.Println("Microsoft Visio is not required; provide a .vssx file and import the result into draw.io.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  wedraw <input.vssx> [-o output.drawio-library.xml] [--mode faithful|ungroup] [--label-gap pixels] [--resolution pixels]")
	fmt.Println("  wedraw inspect <input.vssx>")
	fmt.Println("  wedraw debug <input.vssx> --master \"Master Name\" --out <directory>")
	fmt.Println("  wedraw convert <input.vssx> [-o output.drawio-library.xml] [--mode faithful|ungroup] [--label-gap pixels] [--resolution pixels]")
	fmt.Println("           (the explicit convert command is optional)")
	fmt.Println("  wedraw --version")
	fmt.Println()
	fmt.Println("Modes:")
	fmt.Println("  faithful  preserve artwork as one complete vector layer and keep its label editable (default)")
	fmt.Println("  ungroup   expose safe native paths/groups; keep unsupported artwork as an SVG fallback")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -h, --help       show this help")
	fmt.Println("  -V, --version    show the wedraw version")
	fmt.Println("  -v, --verbose    print each shape while converting")
	fmt.Println("  --resolution px  EMF raster long edge; default 2048, range 256-8192")
	fmt.Printf("Copyright (c) 2026 %s\n", author)
}

func debug(args []string) error {
	if len(args) < 1 {
		return errors.New("debug requires an input .vssx file")
	}
	input := args[0]
	masterName, output := "", "debug-master"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--master":
			if i+1 >= len(args) {
				return errors.New("--master requires a name")
			}
			i++
			masterName = args[i]
		case "--out":
			if i+1 >= len(args) {
				return errors.New("--out requires a directory")
			}
			i++
			output = args[i]
		default:
			return fmt.Errorf("unknown debug option %q", args[i])
		}
	}
	if masterName == "" {
		return errors.New("debug requires --master")
	}
	stencil, cleanup, err := visio.OpenStencil(input)
	if err != nil {
		return fmt.Errorf("open stencil: %w", err)
	}
	defer cleanup()
	defer closeStencil(stencil)
	if err := visio.ConvertEmbeddedMetafiles(stencil); err != nil {
		return fmt.Errorf("recover embedded vector artwork: %w", err)
	}
	master := stencil.GetMaster(masterName)
	if master == nil {
		return fmt.Errorf("master %q not found", masterName)
	}
	if err := visio.ExportMasterDebug(stencil, master, output); err != nil {
		return fmt.Errorf("export debug master: %w", err)
	}
	fmt.Printf("Debug master exported to %s\n", output)
	return nil
}

func printVersion() {
	fmt.Printf("wedraw %s\nCopyright (c) 2026 %s\n", version, author)
}

func inspect(args []string) error {
	if len(args) != 1 {
		return errors.New("inspect requires exactly one input .vssx file")
	}
	stencil, cleanup, err := visio.OpenStencil(args[0])
	if err != nil {
		return fmt.Errorf("open stencil: %w", err)
	}
	defer cleanup()
	defer closeStencil(stencil)

	masters := stencil.Masters
	// Inspect is read-only as well; avoid retaining the original ZIP byte map
	// while walking the parsed master documents.
	stencil.ZipFileContents = nil
	fmt.Printf("File: %s\n", filepath.Base(args[0]))
	fmt.Printf("Shapes: %d\n", len(masters))
	for _, master := range masters {
		anchors, anchorErr := visio.MasterAnchors(master)
		if anchorErr != nil {
			return fmt.Errorf("read anchors for %q: %w", master.Name(), anchorErr)
		}
		fmt.Printf("- %s: %d elements, %d connection points\n", library.SanitizeTitle(master.Name()), len(master.AllShapes()), len(anchors))
	}
	return nil
}

func convert(args []string) error {
	resolution, err := parseRasterResolution(args)
	if err != nil {
		return err
	}
	out, input, mode, labelGap, verbose, showHelp, showVersion, err := parseConvertArgs(args)
	if err != nil {
		return err
	}
	if showVersion {
		printVersion()
		return nil
	}
	if showHelp {
		usage()
		return nil
	}
	if input == "" {
		return errors.New("convert requires exactly one input .vssx file")
	}
	library.SetLabelGap(labelGap)
	visio.SetMetafileResolution(resolution)
	stencil, cleanup, err := visio.OpenStencil(input)
	if err != nil {
		return fmt.Errorf("open stencil: %w", err)
	}
	defer cleanup()
	if err := visio.ConvertEmbeddedMetafiles(stencil); err != nil {
		return fmt.Errorf("recover embedded vector artwork: %w", err)
	}
	defer closeStencil(stencil)

	output := out
	if output == "" {
		base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		output = filepath.Join(filepath.Dir(input), base+".drawio-library.xml")
	}
	masters := stencil.Masters
	if len(masters) == 0 {
		return fmt.Errorf("stencil %q contains no master shapes", input)
	}

	fmt.Fprintf(os.Stderr, "Opening stencil: %s\n", filepath.Base(input))
	fmt.Fprintf(os.Stderr, "Found %d shapes.\n", len(masters))
	fmt.Fprintln(os.Stderr, "Resolving formulas and master inheritance...")
	fmt.Fprintln(os.Stderr, "Recovering spline geometry and preserving vector graphics...")
	fmt.Fprintln(os.Stderr, "Preserving colors, labels, and connection points...")

	file, temporary, err := createTemporaryOutput(output)
	if err != nil {
		return fmt.Errorf("create %s: %w", output, err)
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(temporary)
		}
	}()
	writer, err := library.NewWriter(file, filepath.Base(input))
	if err != nil {
		return fmt.Errorf("start library writer: %w", err)
	}
	converted := 0
	for _, master := range masters {
		name := master.Name()
		displayName := visio.MasterLabel(master)
		if verbose {
			fmt.Fprintf(os.Stderr, "Converting shape: %s\n", library.SanitizeTitle(displayName))
		}
		var rendered *vsdx.SVGResult
		complexMaster := false
		if artwork, artworkWidth, artworkHeight, found, artworkErr := visio.MasterEmbeddedArtwork(stencil, master); artworkErr != nil {
			return fmt.Errorf("find embedded artwork for %q: %w", name, artworkErr)
		} else if found {
			complexMaster = true
			scale := math.Min(240/artworkWidth, 180/artworkHeight)
			rendered = &vsdx.SVGResult{SVG: artwork, Width: artworkWidth * scale, Height: artworkHeight * scale}
		} else {
			rendered, err = vsdx.MasterToSVG(master, vsdx.WithSize(240, 180), vsdx.WithPrecision(2))
			if err != nil {
				return fmt.Errorf("render %q: %w", name, err)
			}
		}
		svg, err := visio.ShiftSVGText(rendered.SVG, visio.PrimaryTextMargin(master, rendered.Width))
		if err != nil {
			return fmt.Errorf("align text for %q: %w", name, err)
		}
		svg, expandedWidth, expandedHeight, err := visio.ExpandSVGForText(svg, rendered.Width, rendered.Height)
		if err != nil {
			return fmt.Errorf("preserve text bounds for %q: %w", name, err)
		}
		svg, expandedWidth, expandedHeight, err = visio.NormalizeSVGViewBox(svg, expandedWidth, expandedHeight)
		if err != nil {
			return fmt.Errorf("normalize graphic bounds for %q: %w", name, err)
		}
		width, height := int(math.Ceil(expandedWidth)), int(math.Ceil(expandedHeight))
		if width < 1 {
			width = 240
		}
		if height < 1 {
			height = 180
		}
		anchors, err := visio.MasterAnchors(master)
		if err != nil {
			return fmt.Errorf("read anchors for %q: %w", name, err)
		}
		points := make([][2]float64, 0, len(anchors))
		originalWidth := rendered.Width
		originalHeight := rendered.Height
		for _, anchor := range anchors {
			x := anchor.X
			y := anchor.Y
			if expandedWidth > 0 {
				x *= originalWidth / expandedWidth
			}
			if expandedHeight > 0 {
				y *= originalHeight / expandedHeight
			}
			points = append(points, [2]float64{x, y})
		}
		var entry library.Entry
		// Both modes keep the label as a separate editable cell. The faithful
		// mode keeps all artwork together; ungroup exposes only geometry that
		// the resolved render tree can represent without losing fidelity.
		switch mode {
		case "faithful":
			entry, err = library.NewSVGGroupEntry(
				library.SanitizeTitle(displayName), width, height, svg, library.PointsStyle(points))
		case "ungroup":
			if complexMaster {
				// Complex EMF/Foreign masters must remain whole. Their child
				// coordinates are not safe to flatten into native cells yet.
				entry, err = library.NewSVGGroupEntry(
					library.SanitizeTitle(displayName), width, height, svg, library.PointsStyle(points))
				break
			}
			rootShapes := master.ChildShapes()
			if len(rootShapes) == 0 {
				err = fmt.Errorf("master has no root shape")
			}
			if len(rootShapes) > 0 {
				renderTree := vsdx.NewRenderTreeBuilder(rootShapes[0]).WithOptions(vsdx.RenderTreeOptions{
					Width: rendered.Width, Height: rendered.Height, Precision: 2,
				}).Build()
				if library.RenderTreeIsSimple(renderTree) {
					entry, err = library.NewFlatRenderTreeUngroupEntry(
						library.SanitizeTitle(displayName), width, height,
						int(math.Ceil(expandedWidth)), int(math.Ceil(expandedHeight)),
						renderTree, library.PointsStyle(points))
				} else {
					entry, err = library.NewSVGGroupEntry(
						library.SanitizeTitle(displayName), width, height, svg, library.PointsStyle(points))
				}
			}
			if err != nil {
				// Keep the complete SVG when native decomposition is not proven safe.
				entry, err = library.NewSVGGroupEntry(
					library.SanitizeTitle(displayName), width, height, svg, library.PointsStyle(points))
			}
		}
		if err != nil {
			return fmt.Errorf("build draw.io stencil for %q: %w", name, err)
		}
		if err := writer.WriteEntry(entry); err != nil {
			return fmt.Errorf("write %q: %w", name, err)
		}
		converted++
		printProgress(converted, len(masters))
	}
	// Foreign images (EMF/SVG/bitmap media) are resolved lazily from the
	// original ZIP members while rendering. Release them only after every
	// master has been processed.
	stencil.ZipFileContents = nil
	fmt.Fprintln(os.Stderr)
	if err := writer.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", output, err)
	}
	if err := replaceOutput(temporary, output); err != nil {
		return fmt.Errorf("finalize %s: %w", output, err)
	}
	complete = true
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	fmt.Fprintf(os.Stderr, "Memory snapshot: %.1f MB allocated.\n", float64(memory.Alloc)/(1024*1024))
	fmt.Printf("Done. Converted %d of %d shapes.\n", converted, len(masters))
	fmt.Printf("Output: %s\n", output)
	fmt.Println("Open draw.io → File → Import Library → select the output file.")
	return nil
}

func parseRasterResolution(args []string) (int, error) {
	resolution := 2048
	for i := 0; i < len(args); i++ {
		if args[i] != "--resolution" {
			continue
		}
		if i+1 >= len(args) {
			return 0, errors.New("--resolution requires a number from 256 to 8192 pixels")
		}
		parsed, err := strconv.Atoi(args[i+1])
		if err != nil || parsed < 256 || parsed > 8192 {
			return 0, errors.New("--resolution requires a number from 256 to 8192 pixels")
		}
		resolution = parsed
		i++
	}
	return resolution, nil
}

func parseConvertArgs(args []string) (output, input, mode string, labelGap int, verbose, showHelp, showVersion bool, err error) {
	mode = "faithful"
	labelGap = 6
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--out":
			if i+1 >= len(args) {
				return "", "", "", 0, false, false, false, errors.New("-o/--out requires a file path")
			}
			output = args[i+1]
			i++
		case "-h", "--help":
			return "", "", mode, labelGap, verbose, true, false, nil
		case "-V", "--version":
			showVersion = true
		case "-v", "--verbose":
			verbose = true
		case "--label-gap":
			if i+1 >= len(args) {
				return "", "", "", 0, false, false, false, errors.New("--label-gap requires a non-negative number of pixels")
			}
			parsed, parseErr := strconv.Atoi(args[i+1])
			if parseErr != nil || parsed < 0 {
				return "", "", "", 0, false, false, false, errors.New("--label-gap requires a non-negative number of pixels")
			}
			labelGap = parsed
			i++
		case "--mode":
			if i+1 >= len(args) {
				return "", "", "", 0, false, false, false, errors.New("--mode requires faithful or ungroup")
			}
			mode = strings.ToLower(args[i+1])
			if mode == "group" {
				mode = "ungroup"
			}
			if mode != "faithful" && mode != "ungroup" {
				return "", "", "", 0, false, false, false, fmt.Errorf("unknown conversion mode %q; use faithful or ungroup", mode)
			}
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", "", 0, false, false, false, fmt.Errorf("unknown convert option %q", args[i])
			}
			if input != "" {
				return "", "", "", 0, false, false, false, errors.New("convert requires exactly one input .vssx file")
			}
			input = args[i]
		}
	}
	return output, input, mode, labelGap, verbose, false, showVersion, nil
}

func createTemporaryOutput(output string) (*os.File, string, error) {
	directory := filepath.Dir(output)
	prefix := "." + filepath.Base(output) + ".tmp-"
	file, err := os.CreateTemp(directory, prefix)
	if err != nil {
		return nil, "", err
	}
	return file, file.Name(), nil
}

func replaceOutput(temporary, output string) error {
	if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporary, output)
}

func printProgress(current, total int) {
	fmt.Fprintf(os.Stderr, "\rConverting shapes %s", progressBar(current, total))
}

func progressBar(current, total int) string {
	const width = 28
	if total < 1 {
		return "[............................]   0% (0/0)"
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	percent := current * 100 / total
	filled := current * width / total
	if current > 0 && filled == 0 {
		filled = 1
	}
	bar := strings.Repeat("#", filled) + strings.Repeat(".", width-filled)
	return fmt.Sprintf("[%s] %3d%% (%d/%d)", bar, percent, current, total)
}

func closeStencil(stencil *vsdx.Stencil) {
	_ = stencil
}
