param(
  [string]$EasyRoot = $PSScriptRoot,
  [string]$PythonScript = "D:\Outlook\OutlookRegister\main.py",
  [string]$OutlookConfig = "D:\Outlook\OutlookRegister\config.json",
  [string]$ManagementUrl = "http://127.0.0.1:9091",
  [string]$ManagementPassword = ""
)

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference = "Stop"
trap {
  Write-Host ""
  Write-Host "脚本失败：$($_.Exception.Message)" -ForegroundColor Red
  Read-Host "按 Enter 关闭窗口"
  exit 1
}

$exe = Join-Path $EasyRoot "easy_proxies.exe"
$config = Join-Path $EasyRoot "config.yaml"

function Get-ManagementPassword {
  if ($ManagementPassword) { return $ManagementPassword }
  if (!(Test-Path $config)) { return "" }
  $raw = Get-Content -Raw -LiteralPath $config
  $block = [regex]::Match($raw, "(?ms)^management:\s*(.*?)(?=^\S|\z)").Groups[1].Value
  $m = [regex]::Match($block, '(?m)^\s*password:\s*"?([^"#\r\n]*)')
  if ($m.Success) { return $m.Groups[1].Value.Trim() }
  return ""
}

function Wait-EasyReady {
  for ($i = 0; $i -lt 60; $i++) {
    try {
      Invoke-WebRequest -Uri $ManagementUrl -UseBasicParsing -TimeoutSec 2 | Out-Null
      return
    } catch {
      Start-Sleep -Seconds 1
    }
  }
  throw "easy_proxies 启动超时"
}

function Get-AuthHeaders {
  $password = Get-ManagementPassword
  if (!$password) { return @{} }
  $body = @{ password = $password } | ConvertTo-Json
  $resp = Invoke-RestMethod -Uri "$ManagementUrl/api/auth" -Method Post -Body $body -ContentType "application/json"
  if ($resp.token) { return @{ Authorization = "Bearer $($resp.token)" } }
  return @{}
}

function Update-OutlookPortEnd {
  param([hashtable]$Headers)

  if (!(Test-Path $OutlookConfig)) { throw "找不到 $OutlookConfig" }

  $poolNodes = Invoke-RestMethod -Uri "$ManagementUrl/api/nodes/pool" -Headers $Headers
  if ($null -eq $poolNodes) {
    $nodeCount = 0
  } elseif ($poolNodes -is [array]) {
    $nodeCount = $poolNodes.Length
  } else {
    $nodeCount = 1
  }
  $outlook = Get-Content -Raw -LiteralPath $OutlookConfig | ConvertFrom-Json
  if (!$outlook.proxy) { throw "Outlook 配置缺少 proxy 字段" }
  if ($nodeCount -le 0) { throw "节点池为空，无法更新 Outlook 端口范围" }

  $portStart = [int]$outlook.proxy.port_start
  $outlook.proxy.port_end = $portStart + $nodeCount
  $outlook | ConvertTo-Json -Depth 100 | Set-Content -LiteralPath $OutlookConfig -Encoding UTF8
  return $nodeCount
}

function Invoke-SubscriptionRefresh {
  param([hashtable]$Headers)

  $start = Invoke-RestMethod -Uri "$ManagementUrl/api/import/refresh" -Method Post -Headers $Headers -Body (@{ key = "" } | ConvertTo-Json) -ContentType "application/json" -TimeoutSec 30
  $jobId = [string]$start.job_id
  if (!$jobId) { throw "启动订阅刷新任务失败：未返回 job_id" }

  $lastLine = ""
  while ($true) {
    $job = Invoke-RestMethod -Uri "$ManagementUrl/api/import/refresh/jobs/$jobId" -Headers $Headers -TimeoutSec 30
    $line = "刷新进度：URL $($job.done_urls)/$($job.total_urls)，成功 $($job.successful)，失败 $($job.failed)，当前入池 $($job.pool_count)"
    if ($line -ne $lastLine) {
      Write-Host $line
      $lastLine = $line
    }
    if ($job.status -eq "finished" -or $job.status -eq "failed") {
      if ([string]::IsNullOrWhiteSpace($job.status) -or $job.done_urls -lt $job.total_urls) {
        throw "订阅刷新任务提前结束：$($job.status)"
      }
      if ($job.status -eq "failed") {
        $msg = [string]$job.error
        if ([string]::IsNullOrWhiteSpace($msg)) { $msg = "全部订阅链接都未拉取到节点" }
        Write-Host "订阅刷新任务失败：$msg" -ForegroundColor Yellow
      }
      return $job
    }
    Start-Sleep -Milliseconds 500
  }
}

if (!(Test-Path $exe)) { throw "找不到 $exe" }
if (!(Test-Path $config)) { throw "找不到 $config" }
if (!(Test-Path $PythonScript)) { throw "找不到 $PythonScript" }

$running = Get-Process easy_proxies -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exe }
if (!$running) {
  $cmd = "/c `"`"$exe`" -config `"$config`" 1>NUL 2>NUL`""
  Start-Process -FilePath "cmd.exe" -ArgumentList $cmd -WorkingDirectory $EasyRoot -WindowStyle Hidden
}
Wait-EasyReady
Write-Host "easy_proxies启动成功"

Write-Host "开始刷新订阅"
$headers = Get-AuthHeaders
$refreshJob = Invoke-SubscriptionRefresh -Headers $headers
$poolCount = Update-OutlookPortEnd -Headers $headers
Write-Host "刷新订阅结束"

Write-Host "开始启动python D:\Outlook\OutlookRegister\main.py的脚本"
$pythonWorkDir = Split-Path -Parent $PythonScript
Push-Location $pythonWorkDir
try {
  & python $PythonScript
  $exitCode = $LASTEXITCODE
} finally {
  Pop-Location
}
exit $exitCode
