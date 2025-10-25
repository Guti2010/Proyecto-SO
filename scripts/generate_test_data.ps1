# scripts/generate_test_data.ps1
# Genera archivos sintéticos grandes y determinísticos en ./data
# - big-ints-60mb.txt  (~60 MB)  -> enteros por línea
# - big-ints-120mb.txt (~120 MB) -> enteros por línea
# - mix-logs-20mb.txt  (~20 MB)  -> líneas de texto con palabra clave (para grep/wordcount)

param(
  [int]$Target60MB = 60,
  [int]$Target120MB = 120,
  [int]$Target20MB = 20
)

$ErrorActionPreference = "Stop"
$DataDir = Join-Path (Split-Path -Parent $PSScriptRoot) "data"
if (!(Test-Path $DataDir)) { New-Item -ItemType Directory -Path $DataDir | Out-Null }

function New-RandomIntFile {
  param(
    [Parameter(Mandatory=$true)][string]$Path,
    [Parameter(Mandatory=$true)][int]$TargetMB,
    [int64]$Min = -1000000000,
    [int64]$Max =  1000000000,
    [int]$Seed = 42,
    [int]$LinesPerChunk = 200000
  )
  Write-Host "-> Generando $Path (~$TargetMB MB) con seed=$Seed ..." -ForegroundColor Cyan
  $targetBytes = [int64]$TargetMB * 1MB
  $rng  = [System.Random]::new($Seed)
  $utf8 = [System.Text.Encoding]::UTF8

  $sw = [System.IO.StreamWriter]::new($Path, $false, $utf8, 1MB)
  try {
    $written = 0L
    while ($written -lt $targetBytes) {
      $sb = [System.Text.StringBuilder]::new(8 * $LinesPerChunk)
      for ($i = 0; $i -lt $LinesPerChunk; $i++) {
        # entero uniforme en [Min, Max]
        $val = $Min + [int64]([double]$rng.NextDouble() * ([double]($Max - $Min + 1)))
        [void]$sb.Append($val).Append("`n")
      }
      $block = $sb.ToString()
      $sw.Write($block)
      $written += $utf8.GetByteCount($block)
      if ($written -ge $targetBytes) { break }
    }
  }
  finally {
    $sw.Flush(); $sw.Dispose()
  }

  $final = (Get-Item $Path).Length
  Write-Host ("   Hecho: {0} bytes (~{1:N1} MB)" -f $final, ($final/1MB)) -ForegroundColor Green
}

function New-PatternLogFile {
  param(
    [Parameter(Mandatory=$true)][string]$Path,
    [Parameter(Mandatory=$true)][int]$TargetMB,
    [string]$Keyword = "ERROR",
    [int]$EveryK = 37,
    [int]$Seed = 7,
    [int]$LinesPerChunk = 150000
  )
  Write-Host "-> Generando $Path (~$TargetMB MB) con seed=$Seed; palabra='$Keyword' ..." -ForegroundColor Cyan
  $targetBytes = [int64]$TargetMB * 1MB
  $rng  = [System.Random]::new($Seed)
  $utf8 = [System.Text.Encoding]::UTF8

  $sw = [System.IO.StreamWriter]::new($Path, $false, $utf8, 1MB)
  try {
    $written = 0L
    $lineNo = 1
    while ($written -lt $targetBytes) {
      $sb = [System.Text.StringBuilder]::new(128 * $LinesPerChunk)
      for ($i = 0; $i -lt $LinesPerChunk; $i++) {
        $ts = [DateTime]::UtcNow.AddSeconds(-$lineNo).ToString("yyyy-MM-ddTHH:mm:ssZ")
        $lvl = if (($lineNo % $EveryK) -eq 0) { $Keyword } else { @("INFO","DEBUG","WARN")[$rng.Next(0,3)] }
        # ID de evento hex (0..65535) -> usa 0x10000 como tope: (1 -shl 16)
        $evtHex = [Convert]::ToString($rng.Next(0, (1 -shl 16)), 16)
        $msg = "evt=$evtHex; user=u$($rng.Next(1000,9999)); val=$($rng.Next(-100000,100000))"
        [void]$sb.Append("[$ts] [$lvl] line=$lineNo ").Append($msg).Append("`n")
        $lineNo++
      }
      $block = $sb.ToString()
      $sw.Write($block)
      $written += $utf8.GetByteCount($block)
      if ($written -ge $targetBytes) { break }
    }
  }
  finally {
    $sw.Flush(); $sw.Dispose()
  }

  $final = (Get-Item $Path).Length
  Write-Host ("   Hecho: {0} bytes (~{1:N1} MB)" -f $final, ($final/1MB)) -ForegroundColor Green
}

# Archivos a generar
$File60  = Join-Path $DataDir "big-ints-60mb.txt"
$File120 = Join-Path $DataDir "big-ints-120mb.txt"
$FileLog = Join-Path $DataDir "mix-logs-20mb.txt"

New-RandomIntFile -Path $File60  -TargetMB $Target60MB  -Seed 42 -Min -1000000000 -Max 1000000000
New-RandomIntFile -Path $File120 -TargetMB $Target120MB -Seed 99 -Min -2000000000 -Max 2000000000
New-PatternLogFile -Path $FileLog -TargetMB $Target20MB -Keyword "ERROR" -EveryK 37 -Seed 7

Write-Host "`nArchivos listos en: $DataDir" -ForegroundColor Cyan
Write-Host "  - $([System.IO.Path]::GetFileName($File60))"
Write-Host "  - $([System.IO.Path]::GetFileName($File120))"
Write-Host "  - $([System.IO.Path]::GetFileName($FileLog))"
