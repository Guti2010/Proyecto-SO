# scripts/test_cpu.ps1
# Pruebas CPU-bound: /isprime, /factor, /pi, /mandelbrot, /matrixmul
# - Casos OK y error de parámetros
# - "Timeout demo" para cada comando (requiere TIMEOUT_CPU pequeño en el server)
#
# NOTA para TIMEOUTS:
#   Para que los pasos "Timeout demo" resulten en 503/timeout, levanta el server con:
#     TIMEOUT_CPU=1s   # o 2s
#   (en docker-compose.yml -> environment, y recrear el servicio).
#
# Uso:
#   .\scripts\test_cpu.ps1

$BASE = "http://localhost:8080"

function Step {
  param(
    [string]$Title,
    [string]$CmdOk,
    [string]$CmdErr = ""
  )
  Write-Host ""
  Write-Host "==========================================" -ForegroundColor Cyan
  Write-Host $Title -ForegroundColor Yellow
  Write-Host "OK  : $CmdOk" -ForegroundColor Green
  if ($CmdErr) { Write-Host "ERR : $CmdErr" -ForegroundColor DarkRed }
  [void](Read-Host "Presiona Enter para ejecutar")

  Write-Host "`n--- OK ---" -ForegroundColor Green
  iex $CmdOk

  if ($CmdErr) {
    Write-Host "`n--- ERROR (esperado) ---" -ForegroundColor DarkRed
    iex $CmdErr
  }

  [void](Read-Host "`n(Enter para continuar)")
}

function StepList {
  param(
    [string]$Title,
    [string[]]$Cmds
  )
  Write-Host ""
  Write-Host "==========================================" -ForegroundColor Cyan
  Write-Host $Title -ForegroundColor Yellow
  $i = 1
  foreach ($c in $Cmds) {
    Write-Host ("[" + $i + "] " + $c) -ForegroundColor Green
    $i++
  }
  [void](Read-Host "Presiona Enter para ejecutar")
  $i = 1
  foreach ($c in $Cmds) {
    Write-Host ("`n--- [" + $i + "] ---") -ForegroundColor Green
    iex $c
    $i++
  }
  [void](Read-Host "`n(Enter para continuar)")
}

# -------------------------
# 1) /isprime
# -------------------------
Step `
  "1) /isprime OK (method=division)" `
  "curl.exe --http1.0 -i `"$BASE/isprime?n=97&method=division`"" `
  "curl.exe --http1.0 -i `"$BASE/isprime`""

Step `
  "2) /isprime OK (method=miller-rabin) y error de método" `
  "curl.exe --http1.0 -i `"$BASE/isprime?n=2147483647&method=miller-rabin`"" `
  "curl.exe --http1.0 -i `"$BASE/isprime?n=97&method=foo`""

# Timeout demo (requiere TIMEOUT_CPU pequeño, p.ej. 1s)
# n grande con división fuerza más trabajo
Step `
  "3) /isprime TIMEOUT demo (method=division, n grande)" `
  "curl.exe --http1.0 -i `"$BASE/isprime?n=2305843009213693951&method=division`""  # Mersenne big (hará mucho trabajo)

# -------------------------
# 2) /factor
# -------------------------
Step `
  "4) /factor OK" `
  "curl.exe --http1.0 -i `"$BASE/factor?n=360`"" `
  "curl.exe --http1.0 -i `"$BASE/factor?n=1`""

Step `
  "5) /factor faltante (error)" `
  "curl.exe --http1.0 -i `"$BASE/factor?n=12`"" `
  "curl.exe --http1.0 -i `"$BASE/factor`""

# Timeout demo (requiere TIMEOUT_CPU pequeño)
# Semiprimo relativamente grande puede tardar en división
Step `
  "6) /factor TIMEOUT demo (n grande/semiprimo)" `
  "curl.exe --http1.0 -i `"$BASE/factor?n=32416190071`""   # ajusta si no timeoutea con tu TIMEOUT_CPU

# -------------------------
# 3) /pi
# -------------------------
Step `
  "7) /pi OK (default chudnovsky)" `
  "curl.exe --http1.0 -i `"$BASE/pi?digits=50`"" `
  "curl.exe --http1.0 -i `"$BASE/pi?digits=0`""

Step `
  "8) /pi OK (spigot) y error de método" `
  "curl.exe --http1.0 -i `"$BASE/pi?digits=40&method=spigot`"" `
  "curl.exe --http1.0 -i `"$BASE/pi?digits=20&method=foo`""

# Timeout demo (requiere TIMEOUT_CPU pequeño)
Step `
  "9) /pi TIMEOUT demo (digits grande, chudnovsky)" `
  "curl.exe --http1.0 -i `"$BASE/pi?digits=5000&method=chudnovsky`""

# -------------------------
# 4) /mandelbrot
# -------------------------
Step `
  "10) /mandelbrot OK" `
  "curl.exe --http1.0 -i `"$BASE/mandelbrot?width=64&height=48&max_iter=200`"" `
  "curl.exe --http1.0 -i `"$BASE/mandelbrot?width=a&height=48&max_iter=200`""

Step `
  "11) /mandelbrot error por parámetros <= 0" `
  "curl.exe --http1.0 -i `"$BASE/mandelbrot?width=64&height=48&max_iter=100`"" `
  "curl.exe --http1.0 -i `"$BASE/mandelbrot?width=64&height=0&max_iter=200`""

# Timeout demo (requiere TIMEOUT_CPU pequeño)
Step `
  "12) /mandelbrot TIMEOUT demo (imagen grande + max_iter alto)" `
  "curl.exe --http1.0 -i `"$BASE/mandelbrot?width=512&height=512&max_iter=2000`""

# -------------------------
# 5) /matrixmul
# -------------------------
Step `
  "13) /matrixmul OK" `
  "curl.exe --http1.0 -i `"$BASE/matrixmul?size=64&seed=42`"" `
  "curl.exe --http1.0 -i `"$BASE/matrixmul?size=0&seed=42`""

Step `
  "14) /matrixmul error de seed" `
  "curl.exe --http1.0 -i `"$BASE/matrixmul?size=32&seed=7`"" `
  "curl.exe --http1.0 -i `"$BASE/matrixmul?size=32&seed=abc`""

# Timeout demo (requiere TIMEOUT_CPU pequeño)
Step `
  "15) /matrixmul TIMEOUT demo (size grande)" `
  "curl.exe --http1.0 -i `"$BASE/matrixmul?size=1100&seed=1`""

Write-Host "`nFin de pruebas CPU-bound." -ForegroundColor Cyan
