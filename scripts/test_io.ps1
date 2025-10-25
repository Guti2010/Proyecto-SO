# scripts/test_io.ps1
# Pruebas I/O: /wordcount, /grep, /hashfile, /sortfile (merge|quick), /compress (gzip|xz)

$BASE = "http://localhost:8080"

# Tus archivos reales
$NUMS_60 = "big-ints-60mb.txt"
$NUMS_120 = "big-ints-120mb.txt"
$LOGS_20 = "mix-logs-20mb.txt"

function Step {
  param([string]$Title,[string]$CmdOk,[string]$CmdErr = "")
  Write-Host "`n==========================================" -ForegroundColor Cyan
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

# 1) /wordcount (logs 20MB)
Step `
  "1) /wordcount (mix-logs-20mb.txt)" `
  "curl.exe --http1.0 -i `"$BASE/wordcount?name=$LOGS_20`"" `
  "curl.exe --http1.0 -i `"$BASE/wordcount?name=NO_EXISTE.txt`""

# 2) /grep en logs (buscar 'ERROR')
Step `
  "2) /grep (pattern=ERROR) en mix-logs-20mb.txt" `
  "curl.exe --http1.0 -i `"$BASE/grep?name=$LOGS_20&pattern=ERROR`"" `
  "curl.exe --http1.0 -i `"$BASE/grep?name=$LOGS_20&pattern=(*invalid`""

# 3) /hashfile (sha256 de números 60MB)
Step `
  "3) /hashfile (sha256 de big-ints-60mb.txt)" `
  "curl.exe --http1.0 -i `"$BASE/hashfile?name=$NUMS_60&algo=sha256`"" `
  "curl.exe --http1.0 -i `"$BASE/hashfile?name=$NUMS_60&algo=md5`""

# 4) /sortfile (merge) recomendado para 120MB
Step `
  "4) /sortfile (merge) big-ints-120mb.txt" `
  "curl.exe --http1.0 -i `"$BASE/sortfile?name=$NUMS_120&algo=merge`"" `
  "curl.exe --http1.0 -i `"$BASE/sortfile?name=NO_EXISTE.txt&algo=merge`""

# 5) /sortfile (quick) sobre 60MB (si tu RAM lo permite; si no, usa merge)
Step `
  "5) /sortfile (quick) big-ints-60mb.txt" `
  "curl.exe --http1.0 -i `"$BASE/sortfile?name=$NUMS_60&algo=quick`"" `
  "curl.exe --http1.0 -i `"$BASE/sortfile?name=$NUMS_60&algo=foo`""

# 6) /compress gzip y xz
Step `
  "6) /compress (gzip) big-ints-60mb.txt" `
  "curl.exe --http1.0 -i `"$BASE/compress?name=$NUMS_60&codec=gzip`"" `
  "curl.exe --http1.0 -i `"$BASE/compress?name=$NUMS_60&codec=zip`""

Step `
  "7) /compress (xz) mix-logs-20mb.txt (requiere xz-utils en el contenedor)" `
  "curl.exe --http1.0 -i `"$BASE/compress?name=$LOGS_20&codec=xz`"" `
  "curl.exe --http1.0 -i `"$BASE/compress?name=$LOGS_20&codec=lz4`""

# 8) 404 de cortesía
Step `
  "8) 404 (ruta inexistente)" `
  "curl.exe --http1.0 -i `"$BASE/no-such-io-route`""
