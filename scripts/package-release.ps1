param(
    [ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version = '2.3.0',
    [switch]$SkipFrontend
)
$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$packageRoot = Join-Path $repoRoot "dist/artex-$Version-windows-amd64"
$archivePath = "$packageRoot.zip"
if ((Test-Path -LiteralPath $packageRoot) -or (Test-Path -LiteralPath $archivePath)) {
    throw 'Output already exists. Save and move it before rebuilding this version.'
}
Push-Location $repoRoot
$savedCgo = $env:CGO_ENABLED
$savedMock = $env:NEXT_PUBLIC_MOCK
try {
    $env:NEXT_PUBLIC_MOCK = '0'
    if (!$SkipFrontend) {
        Push-Location (Join-Path $repoRoot 'web')
        try {
            & npm.cmd ci --no-audit --no-fund
            if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
            & npm.cmd run build:static
            if ($LASTEXITCODE -ne 0) { throw 'Frontend export failed' }
        } finally { Pop-Location }
        $embeddedRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot 'server/webui/dist'))
        if (!$embeddedRoot.StartsWith($repoRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Invalid embedded output path'
        }
        if (Test-Path -LiteralPath $embeddedRoot) { Remove-Item -LiteralPath $embeddedRoot -Recurse -Force }
        New-Item -ItemType Directory -Path $embeddedRoot -Force | Out-Null
        Copy-Item -Path (Join-Path $repoRoot 'web/out/*') -Destination $embeddedRoot -Recurse
    }
    if (!(Test-Path -LiteralPath (Join-Path $repoRoot 'server/webui/dist/index.html'))) {
        throw 'Missing static frontend. Build it before using -SkipFrontend.'
    }
    New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null
    $env:CGO_ENABLED = '0'
    & go build -trimpath -tags embedui -ldflags "-s -w -buildid= -X main.version=$Version" -o (Join-Path $packageRoot 'artex.exe') ./cmd/artex
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    foreach ($name in @('start.bat', 'config.example.json', 'README.md', 'LICENSE', 'THIRD_PARTY_NOTICES.md')) {
        Copy-Item -LiteralPath (Join-Path $repoRoot $name) -Destination $packageRoot
    }
    # Copy only committed skill files: never runtime additions, node_modules or browser state.
    $skillFiles = & git -C $repoRoot ls-files -- skills
    if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate committed skills' }
    foreach ($relative in $skillFiles) {
        if ($relative -match '/validation-[^/]+\.json$') { continue }
        $target = Join-Path $packageRoot $relative
        New-Item -ItemType Directory -Path (Split-Path $target) -Force | Out-Null
        Copy-Item -LiteralPath (Join-Path $repoRoot $relative) -Destination $target
    }
    Copy-Item -LiteralPath (Join-Path $repoRoot 'screenshots') -Destination $packageRoot -Recurse
    $licenseRoot = Join-Path $packageRoot 'licenses'
    New-Item -ItemType Directory -Path $licenseRoot -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $repoRoot 'web/LICENSE') -Destination (Join-Path $licenseRoot 'web-LICENSE')
    Copy-Item -LiteralPath (Join-Path $repoRoot 'web/src/lib/fonts/assets/Outfit-LICENSE.txt') -Destination $licenseRoot
    Compress-Archive -LiteralPath $packageRoot -DestinationPath $archivePath -CompressionLevel Optimal
    $digest = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    "$digest  $([IO.Path]::GetFileName($archivePath))" | Set-Content -LiteralPath (Join-Path $repoRoot 'dist/SHA256SUMS') -Encoding ascii
    Write-Output $archivePath
} finally {
    $env:CGO_ENABLED = $savedCgo
    $env:NEXT_PUBLIC_MOCK = $savedMock
    Pop-Location
}
