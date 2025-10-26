# run_all_tests.ps1
# Ejecuta en cadena TODOS los scripts .ps1 sin pedir Enter.
# Simula "Enter" vía RedirectStandardInput y al final imprime un resumen PASS/FAIL.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# Descubre pwsh si existe; si no, usa powershell clásico
$pwsh = (Get-Command 'pwsh.exe' -ErrorAction SilentlyContinue)
if (-not $pwsh) { $pwsh = Get-Command 'powershell.exe' }

# Carpeta de los scripts
$scriptsDir = $PSScriptRoot

# Orden de ejecución
$scriptList = @(
  'run_race_tests.ps1',
  'run_queue.ps1',
  'test_basic.ps1',
  'test_files.ps1',
  'test_io.ps1',
  'test_jobs_metrics.ps1',
  'test_cpu.ps1'
)

$results = @()

function Run-InteractiveScript([string]$name) {
  $path = Join-Path $scriptsDir $name
  if (-not (Test-Path $path)) {
    Write-Host "SKIP: $name (no existe)" -ForegroundColor Yellow
    $script:results += [pscustomobject]@{ Step = $name; ExitCode = 0; Skipped = $true }
    return
  }

  Write-Host ""
  Write-Host "==> Ejecutando $name" -ForegroundColor Cyan

  # Prepara el proceso con entrada estándar redirigida
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName               = $pwsh.Source
  $psi.Arguments              = "-NoProfile -ExecutionPolicy Bypass -File `"$path`""
  $psi.UseShellExecute        = $false
  $psi.RedirectStandardInput  = $true
  $psi.RedirectStandardOutput = $false
  $psi.RedirectStandardError  = $false
  $proc = [System.Diagnostics.Process]::Start($psi)

  # "Apretar Enter": mandamos muchas líneas en blanco mientras el script viva
  for ($i=0; $i -lt 1000 -and -not $proc.HasExited; $i++) {
    $proc.StandardInput.WriteLine()
    Start-Sleep -Milliseconds 5
  }
  $proc.StandardInput.Close()
  $proc.WaitForExit()

  $rc = $proc.ExitCode
  if ($rc -ne 0) { Write-Host "FAIL: $name (rc=$rc)" -ForegroundColor Red }
  else           { Write-Host "PASS: $name"       -ForegroundColor Green }

  $script:results += [pscustomobject]@{ Step = $name; ExitCode = $rc; Skipped = $false }
}

# Ejecutar todos los scripts en orden
foreach ($s in $scriptList) { Run-InteractiveScript $s }

# ================= RESUMEN FINAL =================
Write-Host ""
Write-Host "================= SUMMARY =================" -ForegroundColor Cyan
$ok = $true
foreach ($r in $results) {
  $label = if ($r.Skipped) { "SKIP" } elseif ($r.ExitCode -eq 0) { "PASS" } else { "FAIL" }
  $color = switch ($label) { "PASS" {"Green"} "FAIL" {"Red"} "SKIP" {"Yellow"} default {"Gray"} }
  Write-Host ("{0,-28} : {1}" -f $r.Step, $label) -ForegroundColor $color
  if (-not $r.Skipped -and $r.ExitCode -ne 0) { $ok = $false }
}
Write-Host "===========================================" -ForegroundColor Cyan

if ($ok) { exit 0 } else { exit 1 }
