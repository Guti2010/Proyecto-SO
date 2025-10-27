
param(
  [switch]$TotalOnly,
  [switch]$ShowTests,
  [switch]$WithRace
)

$ErrorActionPreference = 'Stop'

# Imagen y ruta del repo (soporta espacios)
$GO_IMAGE = 'golang:1.22'
$REPO     = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$docker   = 'docker'

$go      = '/usr/local/go/bin/go'
$covfile = '/tmp/cover.out'

# arma flags
$raceFlag = if ($WithRace) { "-race " } else { "" }

# go test con cobertura “amplificada”
$testCmd = "$go test ./internal/... ${raceFlag}-covermode=atomic -coverpkg=./internal/... -coverprofile=$covfile -count=1"
if (-not $ShowTests) { $testCmd += " >/dev/null 2>&1" }  # oculta logs de test salvo que pidas verlos

# Resumen de cobertura
$coverCmd = "$go tool cover -func=$covfile"
if ($TotalOnly) { $coverCmd += " | grep '^total:'" }

# Comando POSIX simple (sin 'set -e', todo ASCII)
$inside = "$go clean -testcache && $testCmd && $coverCmd"

# docker run con argumentos separados; usamos bash como entrypoint
$argv = @(
  'run','--rm',
  '--mount', "type=bind,source=`"$REPO`",target=/app",
  '-w','/app',
  '--entrypoint','/bin/bash',
  $GO_IMAGE,
  '-lc', $inside
)

& $docker @argv
exit $LASTEXITCODE
