# scripts/loadtest.ps1  (versión corregida)
param(
  [int]$N = 50,
  [int]$Seconds = 10,
  [string]$BaseUrl = "http://localhost:8080",  # <— antes se llamaba $Host (reservado)
  [int]$WorkersSleep = -1,
  [int]$QueueSleep   = -1,
  [switch]$Rebuild,
  [int]$WaitTimeoutSec = 30
)

$ErrorActionPreference = 'Stop'

# Localiza curl.exe real (no el alias de PowerShell)
$curl = (Get-Command curl.exe -ErrorAction SilentlyContinue)
if (-not $curl) { throw "curl.exe no encontrado en PATH" }

# Opcional: ajustar env y reconstruir/levantar
if ($WorkersSleep -ge 0) { $env:WORKERS_SLEEP = "$WorkersSleep" }
if ($QueueSleep   -ge 0) { $env:QUEUE_SLEEP   = "$QueueSleep" }
if ($PSBoundParameters.ContainsKey('Rebuild')) {
  Write-Host "Levantando con WORKERS_SLEEP=$($env:WORKERS_SLEEP) QUEUE_SLEEP=$($env:QUEUE_SLEEP) ..."
  docker compose up -d --build | Out-Null
}

# Espera activa a que /status responda 200 (HTTP/1.0)
$deadline = (Get-Date).AddSeconds($WaitTimeoutSec)
$ready = $false
while ((Get-Date) -lt $deadline) {
  try {
    $code = & $curl.Source --http1.0 -s -o NUL -w "%{http_code}" "$BaseUrl/status"
    if ($code -eq "200") { $ready = $true; break }
  } catch { }
  Start-Sleep -Milliseconds 300
}
if (-not $ready) {
  throw "Servidor no respondió 200 en /status dentro de $WaitTimeoutSec s. Revisa:  docker compose logs -f http10"
}

# Warmup
& $curl.Source --http1.0 -s "$BaseUrl/sleep?seconds=1" > $null

# Avalancha
$jobs = for ($i = 0; $i -lt $N; $i++) {
  Start-Job {
    param($curlPath, $url)
    & $curlPath --http1.0 -s -o NUL -w "%{http_code}`n" $url
  } -ArgumentList $curl.Source, "$BaseUrl/sleep?seconds=$Seconds"
}
$codes = $jobs | Wait-Job | Receive-Job

Write-Host "`n== Resumen códigos =="
$codes | Group-Object | Sort-Object Name | Format-Table @{N='Code';E={$_.Name}}, Count -AutoSize

# Métricas
Write-Host "`n== Métricas =="
$metricsRaw = & $curl.Source --http1.0 -s "$BaseUrl/metrics"
if ($metricsRaw) {
  $metricsRaw | ConvertFrom-Json | Format-List
} else {
  Write-Warning "No se pudo obtener /metrics"
}
