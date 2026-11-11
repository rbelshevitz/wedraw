param(
    [string]$Fixture = 'D:\Temp\VectaDrawFixtures\Cisco\metric\Stencil-Metric\Cisco_All-Icons_V-1_m.vssx',
    [ValidateSet('faithful', 'ungroup')]
    [string]$Mode = 'faithful',
    [int]$MaxPrivateMB = 512
)

$ErrorActionPreference = 'Stop'
$repo = (Get-Location).Path
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ('wedraw-memory-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null
$exe = Join-Path $tempDir 'wedraw.exe'
$output = Join-Path $tempDir 'library.xml'
$stdout = Join-Path $tempDir 'stdout.txt'
$stderr = Join-Path $tempDir 'stderr.txt'

try {
    if (-not (Test-Path -LiteralPath $Fixture)) {
        throw "Fixture not found: $Fixture"
    }

    $env:GOCACHE = Join-Path $repo '.gocache'
    $env:GOFLAGS = '-trimpath'
    go build -o $exe ./cmd/wedraw

    $process = Start-Process -FilePath $exe -ArgumentList @('convert', $Fixture, '-o', $output, '--mode', $Mode) `
        -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru
    $peakWorkingSet = 0
    $peakPrivate = 0
    while (-not $process.HasExited) {
        $process.Refresh()
        $peakWorkingSet = [Math]::Max($peakWorkingSet, $process.WorkingSet64)
        $peakPrivate = [Math]::Max($peakPrivate, $process.PrivateMemorySize64)
        Start-Sleep -Milliseconds 100
    }
    $process.Refresh()
    $peakWorkingSet = [Math]::Max($peakWorkingSet, $process.WorkingSet64)
    $peakPrivate = [Math]::Max($peakPrivate, $process.PrivateMemorySize64)

    $workingSetMB = [Math]::Round($peakWorkingSet / 1MB, 1)
    $privateMB = [Math]::Round($peakPrivate / 1MB, 1)
    Write-Output "Fixture: $([IO.Path]::GetFileName($Fixture))"
    Write-Output "Mode: $Mode"
    Write-Output "Peak Working Set: $workingSetMB MB"
    Write-Output "Peak Private Memory: $privateMB MB"
    Write-Output "Private-memory budget: $MaxPrivateMB MB"

    if ($process.ExitCode -ne 0) {
        Get-Content -LiteralPath $stderr
        throw "wedraw exited with code $($process.ExitCode)"
    }
    if ($privateMB -gt $MaxPrivateMB) {
        throw "Peak private memory exceeded the budget by $([Math]::Round($privateMB - $MaxPrivateMB, 1)) MB"
    }
}
finally {
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
