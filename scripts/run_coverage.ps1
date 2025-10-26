param([double]$Min = 0)  # umbral mínimo de cobertura 

$ErrorActionPreference = 'Stop'
$GO_IMAGE = 'golang:1.22'
$ROOT_WIN = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$DOCKER   = 'docker run --rm --mount type=bind,source="' + $ROOT_WIN + '",target=/app -w /app --entrypoint /usr/local/go/bin/go ' + $GO_IMAGE

# paths
$COVER_DIR  = Join-Path $ROOT_WIN 'cover'
$COVER_FILE = Join-Path $COVER_DIR 'cover.out'
$COVER_REL  = 'cover/cover.out'
$COVER_HTML = 'cover/cover.html'

# asegurar carpeta cover (y que no sea un archivo)
if (Test-Path $COVER_DIR) {
  $itm = Get-Item $COVER_DIR -Force
  if (-not $itm.PSIsContainer) { Remove-Item $COVER_DIR -Force }
}
if (-not (Test-Path $COVER_DIR)) { New-Item -ItemType Directory -Path $COVER_DIR | Out-Null }
if (Test-Path $COVER_FILE) { Remove-Item $COVER_FILE -Force }

# 0) limpieza cache
Invoke-Expression "$DOCKER clean -testcache" | Out-Null

# 1) tests con cobertura (solo ./internal/...)
Invoke-Expression "$DOCKER test ./internal/... -race -covermode=atomic -coverprofile=$COVER_REL -count=1"

# 2) resumen texto
$covText = Invoke-Expression "$DOCKER tool cover -func=$COVER_REL"
$covText | Out-Host

# 3) (opcional) HTML
Invoke-Expression "$DOCKER tool cover -html=$COVER_REL -o $($COVER_REL -replace 'cover.out','cover.html')" | Out-Null

# 4) validar umbral si se pidió
if ($Min -gt 0) {
  $totalLine = ($covText -split "`r?`n") | Where-Object { $_ -match '^total:\s+\(statements\)\s+([0-9.]+)%' } | Select-Object -Last 1
  if (-not $totalLine) { Write-Host "No se encontró la línea 'total' en el resumen" -ForegroundColor Yellow; exit 1 }
  $pct = [double]($totalLine -replace '.*\s([0-9.]+)%','$1')
  Write-Host ("Cobertura total: {0}%" -f $pct) -ForegroundColor Cyan
  if ($pct -lt $Min) {
    Write-Host ("Cobertura menor a {0}% → FAIL" -f $Min) -ForegroundColor Red
    exit 1
  }
}

Write-Host "Cobertura generada en: $COVER_FILE" -ForegroundColor Green
Write-Host "Reporte HTML: $COVER_HTML" -ForegroundColor Green
exit 0
