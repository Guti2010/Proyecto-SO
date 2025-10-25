# scripts/test_files_min.ps1
# Secuencia mínima para probar /createfile y /deletefile

$BASE = "http://localhost:8080"

function Pause-Enter {
  Write-Host "`n(Enter para continuar)" -NoNewline
  [void][System.Console]::ReadLine()
}

Write-Host "== Pruebas mínimas de archivos ==" -ForegroundColor Cyan

# Limpieza previa silenciosa
curl.exe --http1.0 -s "$BASE/deletefile?name=demo.txt" | Out-Null
curl.exe --http1.0 -s "$BASE/deletefile?name=demo(1).txt" | Out-Null
curl.exe --http1.0 -s "$BASE/deletefile?name=demo(2).txt" | Out-Null

# 1) CREAR (OK)
Write-Host "`n[1] Crear demo.txt (OK)" -ForegroundColor Yellow
$cmd = "curl.exe --http1.0 -i `"$BASE/createfile?name=demo.txt&content=hola&repeat=1`""
Write-Host $cmd -ForegroundColor Green
iex $cmd
Pause-Enter

# 2) CREAR MISMO NOMBRE (ERROR: existe, conflict=fail por defecto → 409)
Write-Host "`n[2] Crear demo.txt otra vez (ERROR esperado: 409 exists)" -ForegroundColor Yellow
$cmd = "curl.exe --http1.0 -i `"$BASE/createfile?name=demo.txt&content=hola`""
Write-Host $cmd -ForegroundColor DarkRed
iex $cmd
Pause-Enter

# 3) OVERWRITE (OK, action=overwritten)
Write-Host "`n[3] Overwrite demo.txt (OK)" -ForegroundColor Yellow
$cmd = "curl.exe --http1.0 -i `"$BASE/createfile?name=demo.txt&content=OVERWRITE&conflict=overwrite`""
Write-Host $cmd -ForegroundColor Green
iex $cmd
Pause-Enter

# 4) AUTORENAME (OK, crea demo(1).txt ó el siguiente libre)
Write-Host "`n[4] Autorename con base demo.txt (OK → crea demo(1).txt / demo(2).txt...)" -ForegroundColor Yellow
$cmd = "curl.exe --http1.0 -i `"$BASE/createfile?name=demo.txt&content=x&conflict=autorename`""
Write-Host $cmd -ForegroundColor Green
iex $cmd
Pause-Enter

# 5) DELETE (OK) y DELETE otra vez (ERROR 404)
Write-Host "`n[5] Eliminar demo.txt (OK) y luego intentar eliminar de nuevo (ERROR 404)" -ForegroundColor Yellow
$cmd1 = "curl.exe --http1.0 -i `"$BASE/deletefile?name=demo.txt`""
$cmd2 = "curl.exe --http1.0 -i `"$BASE/deletefile?name=demo.txt`""
Write-Host $cmd1 -ForegroundColor Green
iex $cmd1
Write-Host "`n(Se vuelve a intentar borrar para forzar 404 not_found)" -ForegroundColor DarkGray
Write-Host $cmd2 -ForegroundColor DarkRed
iex $cmd2
Pause-Enter

Write-Host "`n== Fin de pruebas de archivos ==" -ForegroundColor Cyan
