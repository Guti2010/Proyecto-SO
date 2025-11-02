# scripts/test_cpu_jobs_all.ps1
# Carga workers y colas con /jobs/submit (prioridades high/normal/low).
# Cada llamada usa: curl.exe --http1.0 -i "<URL>"
# NOTA: Ajusta TIMEOUT_CPU y los WORKERS_/QUEUE_ en tu server para ver backpressure.

$BASE = "http://localhost:8080"
$P    = [Environment]::ProcessorCount

# Cantidades (escalables por # núcleos)
$COUNT_HIGH   = 4 * $P     # isprime (division) - cola HIGH
$COUNT_NORMAL = 6 * $P     # factor y pi        - cola NORMAL
$COUNT_LOW    = 4 * $P     # mandelbrot/matrix  - cola LOW

# Para no inundar la consola: descartar cuerpo (COMENTA si quieres ver JSON IDs)
$CURL_BASE = @("--http1.0","-i")

Write-Host ">> Encolando HIGH (/isprime division) x $COUNT_HIGH" -ForegroundColor Cyan
# Primo de 18 dígitos para forzar trabajo con division
$prime18 = 999999999999999989
for($i=0; $i -lt $COUNT_HIGH; $i++){
  & curl.exe @CURL_BASE "$BASE/jobs/submit?task=isprime&n=$prime18&method=division&prio=high"
}

Write-Host "`n>> Encolando NORMAL (/factor semiprimo 18 dígitos) x $COUNT_NORMAL" -ForegroundColor Cyan
# 999999937 * 999999883 = 999999820000007371 (18 dígitos)
$semi18 = 999999820000007371
for($i=0; $i -lt $COUNT_NORMAL; $i++){
  & curl.exe @CURL_BASE "$BASE/jobs/submit?task=factor&n=$semi18&prio=normal"
}

Write-Host "`n>> Encolando NORMAL (/pi chudnovsky 100k dígitos) x $COUNT_NORMAL" -ForegroundColor Cyan
for($i=0; $i -lt $COUNT_NORMAL; $i++){
  & curl.exe @CURL_BASE "$BASE/jobs/submit?task=pi&digits=100000&method=chudnovsky&prio=normal"
}

Write-Host "`n>> Encolando LOW (/mandelbrot 2048x2048@4000) x $COUNT_LOW" -ForegroundColor Cyan
for($i=0; $i -lt $COUNT_LOW; $i++){
  & curl.exe @CURL_BASE "$BASE/jobs/submit?task=mandelbrot&width=2048&height=2048&max_iter=4000&prio=low"
}

Write-Host "`n>> Encolando LOW (/matrixmul size=1500) x $COUNT_LOW" -ForegroundColor Cyan
for($i=0; $i -lt $COUNT_LOW; $i++){
  & curl.exe @CURL_BASE "$BASE/jobs/submit?task=matrixmul&size=1500&seed=1&prio=low"
}

Write-Host "`n>> Snapshot de /metrics (colas, workers, latencias)" -ForegroundColor Yellow
& curl.exe @CURL_BASE "$BASE/metrics"

Write-Host "`n>> Snapshot de /jobs/list" -ForegroundColor Yellow
# (Quita -o NUL si quieres ver el listado completo)
& curl.exe @CURL_BASE "$BASE/jobs/list"

Write-Host "`nListo. Colas y workers deberían estar a tope. Ajusta WORKERS_/QUEUE_ para variar la presión." -ForegroundColor Green
