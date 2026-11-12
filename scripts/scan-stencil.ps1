param(
    [Parameter(Mandatory = $true)]
    [string]$Path
)

Add-Type -AssemblyName System.IO.Compression.FileSystem

function Get-Attr($node, $name) {
    if ($null -eq $node) { return "" }
    $a = $node.Attributes[$name]
    if ($null -eq $a) { return "" }
    return $a.Value
}

function Get-Cell($shape, $name) {
    $cell = $shape.SelectSingleNode("./*[local-name()='Cell'][@N='$name']")
    if ($null -eq $cell) { return 0.0 }
    $value = 0.0
    [double]::TryParse((Get-Attr $cell 'V'), [Globalization.NumberStyles]::Float, [Globalization.CultureInfo]::InvariantCulture, [ref]$value) | Out-Null
    return $value
}

function Get-Depth($shape, $depth = 0) {
    $max = $depth
    foreach ($child in $shape.SelectNodes("./*[local-name()='Shapes']/*[local-name()='Shape']")) {
        $max = [Math]::Max($max, (Get-Depth $child ($depth + 1)))
    }
    return $max
}

$zip = [IO.Compression.ZipFile]::OpenRead((Resolve-Path -LiteralPath $Path))
try {
    $entries = @{}
    foreach ($entry in $zip.Entries) { $entries[$entry.FullName] = $entry }
    $masterEntries = @($zip.Entries | Where-Object { $_.FullName -match '^visio/masters/master\d+\.xml$' })
    $foreign = 0; $geometry = 0; $groups = 0; $maxDepth = 0; $degenerate = 0; $missingRels = 0; $emf = 0; $svg = 0; $png = 0
    foreach ($entry in $masterEntries) {
        $reader = New-Object IO.StreamReader($entry.Open())
        try { $xml = [xml]$reader.ReadToEnd() } finally { $reader.Close() }
        $root = $xml.SelectSingleNode("//*[local-name()='Shapes']/*[local-name()='Shape']")
        if ($null -eq $root) { continue }
        $rw = Get-Cell $root 'Width'; $rh = Get-Cell $root 'Height'
        if ($rh -lt 0.01 -or $rw -lt 0.01) { $degenerate++ }
        $maxDepth = [Math]::Max($maxDepth, (Get-Depth $root))
        $id = [IO.Path]::GetFileNameWithoutExtension($entry.Name).Substring(6)
        if (-not $entries.ContainsKey("visio/masters/_rels/master$id.xml.rels")) { $missingRels++ }
        foreach ($shape in $xml.SelectNodes("//*[local-name()='Shape']")) {
            if ((Get-Attr $shape 'Type') -eq 'Foreign') { $foreign++ }
            if ($null -ne $shape.SelectSingleNode("./*[local-name()='Section'][@N='Geometry']")) { $geometry++ }
            if ((Get-Attr $shape 'Type') -eq 'Group') { $groups++ }
        }
    }
    foreach ($entry in $zip.Entries | Where-Object { $_.FullName -like 'visio/media/*' }) {
        switch ([IO.Path]::GetExtension($entry.Name).ToLowerInvariant()) {
            '.emf' { $emf++ }
            '.svg' { $svg++ }
            '.png' { $png++ }
        }
    }
    [pscustomobject]@{
        File = [IO.Path]::GetFileName($Path)
        Masters = $masterEntries.Count
        Groups = $groups
        Foreign = $foreign
        Geometry = $geometry
        MaxDepth = $maxDepth
        DegenerateRoots = $degenerate
        MissingMasterRels = $missingRels
        MediaEMF = $emf
        MediaSVG = $svg
        MediaPNG = $png
    } | Format-List
} finally { $zip.Dispose() }
