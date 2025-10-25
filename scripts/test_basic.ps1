# test_basic.ps1
# Pruebas bloque básico (sin /status ni /createfile|/deletefile)

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

# 1) /fibonacci?num=N
Step `
  "1) /fibonacci" `
  "curl.exe --http1.0 -i `"$BASE/fibonacci?num=10`"" `
  "curl.exe --http1.0 -i `"$BASE/fibonacci?num=-1`""

# 1b) /fibonacci?num=una_letra
Step `
  "1b) /fibonacci" `
  "curl.exe --http1.0 -i `"$BASE/fibonacci?num=15`"" `
  "curl.exe --http1.0 -i `"$BASE/fibonacci?num=u`""

# 2) /reverse?text=abcdef
Step `
  "2) /reverse" `
  "curl.exe --http1.0 -i `"$BASE/reverse?text=abcdef`"" `
  "curl.exe --http1.0 -i `"$BASE/reverse`""

# 3) /toupper?text=abcd
Step `
  '3) /toupper' `
  "curl.exe --http1.0 -i `"$BASE/toupper?text=hola Mundo`"" `
  "curl.exe --http1.0 -i `"$BASE/toupper`""

# 4) /random?count=n&min=a&max=b  (count inválido)
Step `
  '4) /random (count valido vs invalido)' `
  "curl.exe --http1.0 -i `"$BASE/random?count=5&min=10&max=12`"" `
  "curl.exe --http1.0 -i `"$BASE/random?count=0&min=1&max=3`""

# 4b) /random rango invalido: min > max
Step `
  '4b) /random (rango invalido min>max)' `
  "curl.exe --http1.0 -i `"$BASE/random?count=3&min=5&max=7`"" `
  "curl.exe --http1.0 -i `"$BASE/random?count=3&min=7&max=5`""

# 5) /timestamp
Step `
  '5) /timestamp' `
  "curl.exe --http1.0 -i `"$BASE/timestamp`""

# 6) /hash?text=someinput
Step `
  '6) /hash' `
  "curl.exe --http1.0 -i `"$BASE/hash?text=hola`"" `
  "curl.exe --http1.0 -i `"$BASE/hash`""

# 7) /simulate?seconds=s&task=name (sleep y spin)
Step `
  '7) /simulate (sleep)' `
  "curl.exe --http1.0 -i `"$BASE/simulate?seconds=2&task=sleep`"" `
  "curl.exe --http1.0 -i `"$BASE/simulate?seconds=2&task=foo`""

Step `
  '7b) /simulate (spin)' `
  "curl.exe --http1.0 -i `"$BASE/simulate?seconds=1&task=spin`""

# 8) /sleep?seconds=s
Step `
  '8) /sleep' `
  "curl.exe --http1.0 -i `"$BASE/sleep?seconds=1`"" `
  "curl.exe --http1.0 -i `"$BASE/sleep?seconds=-1`""

# 9) /loadtest?tasks=n&sleep=x
Step `
  '9) /loadtest' `
  "curl.exe --http1.0 -i `"$BASE/loadtest?tasks=5&sleep=1`"" `
  "curl.exe --http1.0 -i `"$BASE/loadtest?tasks=-3&sleep=1`""

# 10) /help
Step `
  '10) /help' `
  "curl.exe --http1.0 -i `"$BASE/help`""

# 11) 404 ruta desconocida
Step `
  '11) 404 (ruta inexistente)' `
  "curl.exe --http1.0 -i `"$BASE/help`"" `
  "curl.exe --http1.0 -i `"$BASE/no-such-route`""

Write-Host "`nFin de pruebas basicas" -ForegroundColor Cyan
