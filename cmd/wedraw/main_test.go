package main

import "testing"

func TestProgressBarBoundaries(t *testing.T) {
	// Keep this test focused on the stable visual format: fixed width,
	// readable percentage and an explicit current/total counter.
	for _, tc := range []struct {
		current int
		total   int
		want    string
	}{
		{0, 10, "[............................]   0% (0/10)"},
		{5, 10, "[##############..............]  50% (5/10)"},
		{10, 10, "[############################] 100% (10/10)"},
	} {
		if got := progressBar(tc.current, tc.total); got != tc.want {
			t.Errorf("progressBar(%d, %d) = %q, want %q", tc.current, tc.total, got, tc.want)
		}
	}
}

func TestParseConvertArgsHelp(t *testing.T) {
	output, input, mode, labelGap, verbose, showHelp, showVersion, err := parseConvertArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("parseConvertArgs returned error: %v", err)
	}
	if output != "" || input != "" || mode != "faithful" || labelGap != 6 || verbose || !showHelp || showVersion {
		t.Fatalf("parseConvertArgs(--help) = output %q, input %q, mode %q, labelGap %d, verbose %v, showHelp %v, showVersion %v", output, input, mode, labelGap, verbose, showHelp, showVersion)
	}
}

func TestParseConvertArgsAllowsOutputBeforeInput(t *testing.T) {
	output, input, mode, labelGap, verbose, showHelp, showVersion, err := parseConvertArgs([]string{"--out", "library.xml", "source.vssx"})
	if err != nil {
		t.Fatalf("parseConvertArgs returned error: %v", err)
	}
	if output != "library.xml" || input != "source.vssx" || mode != "faithful" || labelGap != 6 || verbose || showHelp || showVersion {
		t.Fatalf("parseConvertArgs returned output %q, input %q, mode %q, labelGap %d, verbose %v, showHelp %v, showVersion %v", output, input, mode, labelGap, verbose, showHelp, showVersion)
	}
}

func TestParseConvertArgsUngroupMode(t *testing.T) {
	_, _, mode, labelGap, verbose, showHelp, showVersion, err := parseConvertArgs([]string{"--mode", "ungroup", "source.vssx"})
	if err != nil || verbose || showHelp || showVersion || mode != "ungroup" || labelGap != 6 {
		t.Fatalf("parseConvertArgs ungroup = mode %q, labelGap %d, verbose %v, showHelp %v, showVersion %v, err %v", mode, labelGap, verbose, showHelp, showVersion, err)
	}
}

func TestParseConvertArgsLabelGap(t *testing.T) {
	_, _, _, labelGap, verbose, showHelp, showVersion, err := parseConvertArgs([]string{"--label-gap", "8", "source.vssx"})
	if err != nil || verbose || showHelp || showVersion || labelGap != 8 {
		t.Fatalf("parseConvertArgs label gap = %d, verbose %v, showHelp %v, showVersion %v, err %v", labelGap, verbose, showHelp, showVersion, err)
	}
}

func TestParseConvertArgsVerboseAndVersion(t *testing.T) {
	_, _, _, _, verbose, showHelp, showVersion, err := parseConvertArgs([]string{"-v", "-V"})
	if err != nil || !verbose || showHelp || !showVersion {
		t.Fatalf("parseConvertArgs verbose/version = verbose %v, help %v, version %v, err %v", verbose, showHelp, showVersion, err)
	}
}

func TestParseRasterResolution(t *testing.T) {
	if got, err := parseRasterResolution(nil); err != nil || got != 2048 {
		t.Fatalf("default resolution = %d, err %v", got, err)
	}
	if got, err := parseRasterResolution([]string{"--resolution", "4096"}); err != nil || got != 4096 {
		t.Fatalf("custom resolution = %d, err %v", got, err)
	}
	if _, err := parseRasterResolution([]string{"--resolution", "128"}); err == nil {
		t.Fatal("invalid resolution was accepted")
	}
}
