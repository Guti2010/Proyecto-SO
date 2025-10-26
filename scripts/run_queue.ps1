$BASE = "http://localhost:8080"

function Step {
  param([string]$Title,[string]$CmdOk,[string]$CmdErr = "")
  Write-Host ""
  Write-Host "==========================================" -ForegroundColor Cyan
  Write-Host $Title -ForegroundColor Yellow
  Write-Host "OK  : $CmdOk" -ForegroundColor Green
  if ($CmdErr) { Write-Host "ERR : $CmdErr" -ForegroundColor DarkRed }
  Write-Host "Presiona Enter para ejecutar..." -NoNewline
  [void][System.Console]::ReadLine()

  Write-Host "`n--- OK ---" -ForegroundColor Green
  iex $CmdOk

  if ($CmdErr) {
    Write-Host "`n--- ERROR (esperado) ---" -ForegroundColor DarkRed
    iex $CmdErr
  }

  Write-Host "`n(Enter para continuar)" -NoNewline
  [void][System.Console]::ReadLine()
}

# Docker (montaje robusto en Windows)
$GO_IMAGE = "golang:1.22"
$ROOT_WIN = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$DOCKER   = 'docker run --rm --mount type=bind,source="' + $ROOT_WIN + '",target=/app -w /app --entrypoint /usr/local/go/bin/go ' + $GO_IMAGE

# 0) sanity check
Step "0) go version (sanity check)" ($DOCKER + " version")

# 1) limpiar caché
Step "1) Limpiar cache de tests"     ($DOCKER + " clean -testcache")

Step "2) Test de COLAS (backpressure)" `
     ($DOCKER + " test ./internal/sched -run TestPool_Backpressure -v -count=1")

Write-Host "`nFin de pruebas (QUEUES)" -ForegroundColor Cyan
