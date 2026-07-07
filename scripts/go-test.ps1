param(
    [switch]$Integration,
    [switch]$Race
)

$ErrorActionPreference = "Stop"

$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Push-Location $repo
try {
    $args = @("test", "-count=1")
    if ($Integration) {
        $args += "-tags=integration"
    }
    if ($Race) {
        $args += "-race"
    }
    $args += "./..."

    $go = Get-Command go -ErrorAction SilentlyContinue
    if ($null -ne $go) {
        & go @args
        exit $LASTEXITCODE
    }

    Write-Host "go non trovato in PATH, uso Docker (golang:1.24)..." -ForegroundColor Yellow
    $dockerArgs = @(
        "run", "--rm",
        "-v", "${repo}:/src",
        "-w", "/src",
        "golang:1.24",
        "go"
    ) + $args

    & docker @dockerArgs
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
