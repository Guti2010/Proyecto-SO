# scripts/test_jobs.ps1
# Pruebas de Jobs: prioridad, cancelacion, metrics y backpressure
# Requiere: curl.exe (Windows), servidor en http://localhost:8080
# Nota: sin tildes y sin usar matrixmul (usamos mandelbrot para cancelar en ejecucion)

$BASE = "http://localhost:8080"

function Pause-Step($title) {
  Write-Host ""
  Write-Host "==========================================" -ForegroundColor Cyan
  Write-Host $title -ForegroundColor Yellow
  [void](Read-Host "Presiona Enter para continuar")
}

function Run-Cmd([string]$label, [string]$cmd) {
  Write-Host ""
  Write-Host "---- $label ----" -ForegroundColor Green
  Write-Host $cmd -ForegroundColor DarkGray
  iex $cmd
}

# ------------------------------------------------------------
# 0) Revisar forma de /metrics
# ------------------------------------------------------------
Pause-Step "0) Revisar shape de /metrics (priority_queues y workers)"

$cmd0a = 'curl.exe --http1.0 -s "' + $BASE + '/metrics"'
Run-Cmd "GET /metrics (json bruto)" $cmd0a

Write-Host "`nSugerencia: deberias ver objetos tipo:" -ForegroundColor DarkCyan
Write-Host '  .sleep.priority_queues.high|norm|low  (len, cap)' -ForegroundColor DarkCyan
Write-Host '  .sleep.workers { total, busy, idle }' -ForegroundColor DarkCyan

# ------------------------------------------------------------
# 1) Prioridad: high debe adelantarse a low
# ------------------------------------------------------------
Pause-Step "1) Prioridad: high debe adelantarse a low"

# 1. Ocupar ambos workers con 20s
$busy = @()
for ($i=1; $i -le 2; $i++) {
  $cmd = 'curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=20&timeout_ms=60000" | ConvertFrom-Json'
  $r = iex $cmd
  $busy += $r.job_id
}
Start-Sleep -Seconds 1

# 2. Encolar LOW primero
$low = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=1&prio=low&timeout_ms=60000" | ConvertFrom-Json')
$lowId = $low.job_id

# 3. Encolar HIGH despues
$hi  = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=1&prio=high&timeout_ms=60000" | ConvertFrom-Json')
$hiId = $hi.job_id

# 4. Poll status: HIGH deberia pasar a running antes que LOW
Write-Host "`nPoll de estados (high vs low):" -ForegroundColor DarkCyan
for ($i=0; $i -lt 30; $i++) {
  $sHi  = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/status?id=' + $hiId + '"')
  $sLow = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/status?id=' + $lowId + '"')
  Write-Host $sHi
  Write-Host $sLow
  Write-Host "----"
  if ($sHi -match '"status":"running"') { break }
  Start-Sleep -Seconds 1
}
Write-Host "`nEsperado: high en running mientras low sigue queued." -ForegroundColor Green

# ------------------------------------------------------------
# 2) Cancelacion en cola
# ------------------------------------------------------------
Pause-Step "2) Cancelacion en cola (job queued)"

# Asegura workers ocupados
$busy = @()
for ($i=1; $i -le 2; $i++) {
  [void](iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=25&timeout_ms=60000"'))
}
Start-Sleep -Milliseconds 800

# Encola job que quedara queued
$q = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=10&timeout_ms=60000" | ConvertFrom-Json')
$qid = $q.job_id
Run-Cmd "Status inicial del queued" ('curl.exe --http1.0 -s "' + $BASE + '/jobs/status?id=' + $qid + '"')

# Cancelar de inmediato
Run-Cmd "Cancelar" ('curl.exe --http1.0 -s "' + $BASE + '/jobs/cancel?id=' + $qid + '"')

# Verificar estado final
Run-Cmd "Status final (deberia ser canceled)" ('curl.exe --http1.0 -s "' + $BASE + '/jobs/status?id=' + $qid + '"')

Write-Host "`nEsperado: status=canceled y en /jobs/result error canceled." -ForegroundColor Green

# ------------------------------------------------------------
# 3) Cancelacion en ejecucion (ctx) usando mandelbrot
# ------------------------------------------------------------
Pause-Step "3) Cancelacion en ejecucion (mandelbrot)"

# Lanzar mandelbrot pesado (ajusta si dura poco)
$job = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=mandelbrot&width=512&height=512&max_iter=2000&timeout_ms=120000" | ConvertFrom-Json')
$mid = $job.job_id

# Esperar a running
for ($i=0; $i -lt 15; $i++) {
  $s = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/status?id=' + $mid + '"')
  Write-Host $s
  if ($s -match '"status":"running"') { break }
  Start-Sleep -Seconds 1
}

# Cancelar mientras corre
Run-Cmd "Cancelar en ejecucion" ('curl.exe --http1.0 -s "' + $BASE + '/jobs/cancel?id=' + $mid + '"')

# Poll hasta canceled
for ($i=0; $i -lt 20; $i++) {
  $s = iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/status?id=' + $mid + '"')
  Write-Host $s
  if ($s -match '"status":"canceled"') { break }
  Start-Sleep -Seconds 1
}

# (opcional) ver result
Run-Cmd "Result (deberia contener error canceled)" ('curl.exe --http1.0 -s "' + $BASE + '/jobs/result?id=' + $mid + '"')

Write-Host "`nEsperado: status=canceled con error canceled." -ForegroundColor Green

# ------------------------------------------------------------
# 4) Ver colas por prioridad en /metrics
# ------------------------------------------------------------
Pause-Step "4) Ver colas por prioridad en /metrics"

# Encola 3 low para observar queue len
for ($i=1; $i -le 3; $i++) {
  [void](iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=8&prio=low&timeout_ms=60000"'))
}

Run-Cmd "GET /metrics (low queue)" ('curl.exe --http1.0 -s "' + $BASE + '/metrics"')
Write-Host "`nBusca en el json: .sleep.priority_queues.low len > 0" -ForegroundColor DarkCyan

# ------------------------------------------------------------
# 5) (Opcional) Forzar backpressure 503
# ------------------------------------------------------------
Pause-Step "5) (Opcional) Backpressure: intentar forzar 503"

# Ocupar workers
for ($i=1; $i -le 2; $i++) {
  [void](iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=20&timeout_ms=60000"'))
}
Start-Sleep -Milliseconds 500

# Llenar cola norm (ajusta cantidad segun tu config real)
for ($i=1; $i -le 12; $i++) {
  [void](iex ('curl.exe --http1.0 -s "' + $BASE + '/jobs/submit?task=sleep&seconds=20&timeout_ms=200"'))
}

# Intento extra con timeout corto: deberia pegar 503 por backpressure
Run-Cmd "Intento extra (esperado 503)" ('curl.exe --http1.0 -i "' + $BASE + '/jobs/submit?task=sleep&seconds=1&timeout_ms=200"')

Write-Host "`nFin de pruebas." -ForegroundColor Cyan
