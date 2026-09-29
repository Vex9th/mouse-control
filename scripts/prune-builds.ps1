<#
.SYNOPSIS
按明确归属和回传证据清理旧构建批次，默认只预演。
.DESCRIPTION
仅支持 ProjectRoot/builds/<任务类型>/<批次>。不扫描推断删除目标，不触碰 dependencies。
ManifestPath 是操作者完成归档回传后生成的 JSON。schemaVersion=1，projectId 和
projectRoot 明确项目；keep 列表声明成功批次、artifact 相对路径、sha256；
remove 列表声明 batch、projectId、files[{path,length,sha256}]、
excludedCacheDirectories（仅允许批次根的 cache/.cache/gocache）及 archive：
createdSha256、receivedSha256、destination、verifiedAt。
回传端必须先核对压缩包及全部原始文件；脚本校验此凭据并重新核对源文件，
不会自行证明远端 destination 当前仍可用。清理期间不得并行启动本项目构建。
任意预检失败均不开始删除；执行中失败立即停止，已删除批次可从归档恢复。
.EXAMPLE
.\prune-builds.ps1 -ProjectRoot D:\Developer\MouseControl -KeepBatch gui\release-01 -ManifestPath .\receipt.json
.EXAMPLE
.\prune-builds.ps1 -ProjectRoot D:\Developer\MouseControl -KeepBatch gui\release-01 -ManifestPath .\receipt.json -Execute
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ProjectRoot,
    [Parameter(Mandatory = $true)][string[]]$KeepBatch,
    [Parameter(Mandatory = $true)][string]$ManifestPath,
    [switch]$Execute,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2
if ($Execute -and $DryRun) { throw 'Execute 与 DryRun 不能同时使用。' }
if ($env:OS -ne 'Windows_NT') { throw '此脚本仅在 Windows 本地文件系统运行。' }

function Assert-NoLink([string]$Path) {
    $cursor = Get-Item -LiteralPath $Path -Force
    if ($cursor.PSProvider.Name -ne 'FileSystem') { throw "拒绝非文件系统路径：$Path" }
    while ($null -ne $cursor) {
        if ($cursor.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw "拒绝链接或非文件系统路径：$($cursor.FullName)"
        }
        if ($cursor -is [IO.DirectoryInfo]) { $cursor = $cursor.Parent } else { $cursor = $cursor.Directory }
    }
}
function Get-CanonicalDirectory([string]$Path) {
    if (![IO.Path]::IsPathRooted($Path) -or $Path.StartsWith('\\')) { throw "必须指定本地绝对目录：$Path" }
    Assert-NoLink $Path
    $item = Get-Item -LiteralPath $Path -Force
    if (!$item.PSIsContainer) { throw "不是目录：$Path" }
    return $item.FullName.TrimEnd('\')
}
function Get-RelativePath([string]$Path, [switch]$Batch) {
    if ([string]::IsNullOrWhiteSpace($Path) -or [IO.Path]::IsPathRooted($Path)) { throw "相对路径无效：$Path" }
    $parts = @($Path.Replace('\', '/').Split('/'))
    if ($Batch -and $parts.Count -ne 2) { throw "批次必须是任务类型/批次：$Path" }
    foreach ($part in $parts) {
        if ($part -notmatch '^[^<>:"|?*\x00-\x1F/\\]+$' -or $part -in @('.', '..') -or $part.EndsWith('.') -or $part.EndsWith(' ')) {
            throw "路径包含不安全分段：$Path"
        }
    }
    return ($parts -join '/')
}
function Get-ContainedPath([string]$Base, [string]$Relative) {
    $full = [IO.Path]::GetFullPath((Join-Path $Base $Relative))
    if (!$full.StartsWith($Base.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) { throw "路径越界：$Relative" }
    return $full
}
function Assert-Sha256([string]$Hash) {
    if ($Hash -notmatch '^[0-9a-fA-F]{64}$') { throw 'SHA-256 格式无效。' }
}
function Assert-Idle([string[]]$Paths) {
    # 查询失败必须终止，不能把没有权限读取进程当成空列表。
    $processes = @(Get-CimInstance -ClassName Win32_Process -ErrorAction Stop)
    foreach ($process in $processes) {
        if ($process.ProcessId -eq $PID) { continue }
        foreach ($path in $Paths) {
            $prefix = $path.TrimEnd('\') + '\'
            $exe = [string]$process.ExecutablePath
            $command = ([string]$process.CommandLine).Replace('/', '\')
            if (($exe -and $exe.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) -or
                ($command -and $command.IndexOf($path, [StringComparison]::OrdinalIgnoreCase) -ge 0)) {
                throw "批次正在使用：$path；PID=$($process.ProcessId)，$($process.Name)"
            }
        }
    }
}
function Read-BatchFiles([string]$Path, [string[]]$ExcludedCaches) {
    Assert-NoLink $Path
    $queue = New-Object 'System.Collections.Generic.Queue[object]'
    $files = New-Object 'System.Collections.Generic.List[object]'
    $queue.Enqueue(@{ Path = $Path; Cache = $false })
    while ($queue.Count -gt 0) {
        $next = $queue.Dequeue()
        foreach ($item in Get-ChildItem -LiteralPath $next.Path -Force) {
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "拒绝批次内链接：$($item.FullName)" }
            $isCache = $next.Cache -or ($next.Path -eq $Path -and $item.PSIsContainer -and $item.Name -in $ExcludedCaches)
            if ($item.PSIsContainer) {
                $queue.Enqueue(@{ Path = $item.FullName; Cache = $isCache })
            } elseif (!$isCache) {
                $files.Add([pscustomobject]@{
                    path = $item.FullName.Substring($Path.Length + 1).Replace('\', '/')
                    length = $item.Length
                    sha256 = (Get-FileHash -LiteralPath $item.FullName -Algorithm SHA256).Hash
                })
            }
        }
    }
    return $files.ToArray()
}
function Assert-Receipt($Entry, [string]$Path) {
    $receipt = $Entry.archive
    Assert-Sha256 ([string]$receipt.createdSha256)
    Assert-Sha256 ([string]$receipt.receivedSha256)
    $stamp = [datetime]::MinValue
    if ($receipt.createdSha256 -ne $receipt.receivedSha256 -or
        [string]::IsNullOrWhiteSpace([string]$receipt.destination) -or
        ![datetime]::TryParse([string]$receipt.verifiedAt, [ref]$stamp)) { throw "缺少有效回传证据：$Path" }
    $caches = @($Entry.excludedCacheDirectories)
    foreach ($cache in $caches) {
        if ($cache -notin @('cache', '.cache', 'gocache')) { throw "不允许排除非缓存目录：$cache" }
    }
    $expected = @{}
    foreach ($file in @($Entry.files)) {
        $name = Get-RelativePath ([string]$file.path)
        Assert-Sha256 ([string]$file.sha256)
        if ($expected.ContainsKey($name) -or [long]$file.length -lt 0) { throw "文件清单重复或长度无效：$name" }
        $expected[$name] = $file
    }
    if ($expected.Count -eq 0) { throw "拒绝没有归档文件清单的批次：$Path" }
    $actual = @(Read-BatchFiles $Path $caches)
    if ($actual.Count -ne $expected.Count) { throw "归档后文件数量改变：$Path" }
    foreach ($file in $actual) {
        if (!$expected.ContainsKey($file.path)) { throw "归档后出现新文件：$($file.path)" }
        $old = $expected[$file.path]
        if ($file.length -ne [long]$old.length -or $file.sha256 -ne $old.sha256) { throw "归档后文件变化：$($file.path)" }
    }
    return $actual.Count
}

$root = Get-CanonicalDirectory $ProjectRoot
if ($root -eq [IO.Path]::GetPathRoot($root).TrimEnd('\')) { throw '项目根不能是磁盘根。' }
$builds = Get-CanonicalDirectory (Join-Path $root 'builds')
Assert-NoLink $ManifestPath
$manifest = Get-Content -LiteralPath $ManifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
if ($manifest.schemaVersion -ne 1 -or [string]::IsNullOrWhiteSpace([string]$manifest.projectId)) { throw '归属清单版本或项目 ID 无效。' }
if ((Get-CanonicalDirectory ([string]$manifest.projectRoot)) -ne $root) { throw '清单项目根与实际项目根不一致。' }
$kept = @{}
foreach ($batch in $KeepBatch) {
    $name = Get-RelativePath $batch -Batch
    if ($kept.ContainsKey($name)) { throw "重复保留批次：$name" }
    $entries = @($manifest.keep | Where-Object { (Get-RelativePath ([string]$_.batch) -Batch) -eq $name })
    if ($entries.Count -ne 1 -or $entries[0].status -ne 'succeeded') { throw "保留批次没有唯一成功记录：$name" }
    $entry = $entries[0]
    $path = Get-CanonicalDirectory (Get-ContainedPath $builds $name)
    $artifact = Get-ContainedPath $path (Get-RelativePath ([string]$entry.artifact))
    Assert-NoLink $artifact
    Assert-Sha256 ([string]$entry.sha256)
    if ((Get-FileHash -LiteralPath $artifact -Algorithm SHA256).Hash -ne $entry.sha256) { throw "成功产物哈希不匹配：$name" }
    $kept[$name] = $path
}
if ($kept.Count -eq 0) { throw '必须保留至少一个已核实的成功批次。' }
$planned = @{}
$plan = New-Object 'System.Collections.Generic.List[object]'
foreach ($entry in @($manifest.remove)) {
    $name = Get-RelativePath ([string]$entry.batch) -Batch
    if ($entry.projectId -ne $manifest.projectId) { throw "归属不匹配：$name" }
    if ($kept.ContainsKey($name) -or $planned.ContainsKey($name)) { throw "删除目标重复或与保留批次重叠：$name" }
    $path = Get-CanonicalDirectory (Get-ContainedPath $builds $name)
    $count = Assert-Receipt $entry $path
    $planned[$name] = $true
    $plan.Add([pscustomobject]@{ Batch = $name; Path = $path; ArchivedFiles = $count; Entry = $entry })
}
Assert-Idle @($plan | ForEach-Object { $_.Path })
foreach ($item in $plan) {
    if ($Execute) {
        Assert-Idle @($item.Path)
        $null = Assert-Receipt $item.Entry $item.Path
        Remove-Item -LiteralPath $item.Path -Recurse -Force
        if (Test-Path -LiteralPath $item.Path) { throw "删除未完成：$($item.Path)" }
    }
    [pscustomobject]@{ Action = $(if ($Execute) { 'Deleted' } else { 'DryRun' }); Batch = $item.Batch; ArchivedFiles = $item.ArchivedFiles; Archive = $item.Entry.archive.destination }
}
