[CmdletBinding()]
param([string]$ScriptPath)
$ErrorActionPreference = 'Stop'
if (!$ScriptPath) { $ScriptPath = Join-Path $PSScriptRoot 'prune-builds.ps1' }
$suite = Join-Path $PSScriptRoot ('prune-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $suite | Out-Null
$script:passed = 0
function Assert-True($Condition, [string]$Message) { if (!$Condition) { throw $Message } }
function New-Case([string]$Name) {
    $root = Join-Path $suite $Name
    foreach ($dir in @('builds/task/old', 'builds/task/good', 'builds/task/unknown', 'dependencies')) {
        New-Item -ItemType Directory -Path (Join-Path $root $dir) -Force | Out-Null
    }
    [IO.File]::WriteAllText((Join-Path $root 'builds/task/old/source.txt'), 'old source')
    [IO.File]::WriteAllText((Join-Path $root 'builds/task/good/app.exe'), 'successful artifact')
    [IO.File]::WriteAllText((Join-Path $root 'dependencies/tool.txt'), 'keep dependency')
    $hash = (Get-FileHash -LiteralPath (Join-Path $root 'builds/task/good/app.exe')).Hash
    $files = @([ordered]@{ path = 'source.txt'; length = 10; sha256 = (Get-FileHash -LiteralPath (Join-Path $root 'builds/task/old/source.txt')).Hash })
    $manifest = [ordered]@{
        schemaVersion = 1; projectId = 'fixture'; projectRoot = $root
        keep = @([ordered]@{ batch = 'task/good'; status = 'succeeded'; artifact = 'app.exe'; sha256 = $hash })
        remove = @([ordered]@{ batch = 'task/old'; projectId = 'fixture'; files = $files; excludedCacheDirectories = @(); archive = [ordered]@{ createdSha256 = ('a' * 64); receivedSha256 = ('a' * 64); destination = '/verified/archive.tar.gz'; verifiedAt = '2026-09-29T00:00:00Z' } })
    }
    return @{ Root = $root; Manifest = $manifest; File = (Join-Path $root 'retention.json') }
}
function Invoke-Case($Case, [switch]$Execute) {
    $Case.Manifest | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $Case.File -Encoding UTF8
    & $ScriptPath -ProjectRoot $Case.Root -KeepBatch 'task/good' -ManifestPath $Case.File -Execute:$Execute | Out-Null
}
function Expect-Rejected($Case, [string]$Name) {
    $rejected = $false
    try { Invoke-Case $Case -Execute } catch { $rejected = $true; Write-Host "拒绝 $Name : $($_.Exception.Message)" }
    Assert-True $rejected "应拒绝：$Name"
    Assert-True (Test-Path -LiteralPath (Join-Path $Case.Root 'builds/task/old')) "失败时不得删除旧批次：$Name"
    Assert-True (Test-Path -LiteralPath (Join-Path $Case.Root 'builds/task/good')) "失败时不得删除成功批次：$Name"
    $script:passed++
}
try {
    $c = New-Case 'dry-run'; Invoke-Case $c
    Assert-True (Test-Path -LiteralPath (Join-Path $c.Root 'builds/task/old/source.txt')) '默认预演不得删除文件'
    $script:passed++

    $c = New-Case 'execute'; Invoke-Case $c -Execute
    Assert-True (!(Test-Path -LiteralPath (Join-Path $c.Root 'builds/task/old'))) '执行后应删除明确归属且回传成功的旧批次'
    Assert-True (Test-Path -LiteralPath (Join-Path $c.Root 'builds/task/good/app.exe')) '保留成功批次'
    Assert-True (Test-Path -LiteralPath (Join-Path $c.Root 'builds/task/unknown')) '不删除未声明批次'
    Assert-True (Test-Path -LiteralPath (Join-Path $c.Root 'dependencies/tool.txt')) '不删除依赖'
    $script:passed++

    $c = New-Case 'failed-build'; $c.Manifest.keep[0].status = 'failed'; Expect-Rejected $c '保留批次构建失败'
    $c = New-Case 'wrong-artifact'; $c.Manifest.keep[0].sha256 = 'b' * 64; Expect-Rejected $c '成功产物哈希不匹配'
    $c = New-Case 'no-receipt'; $c.Manifest.remove[0].Remove('archive'); Expect-Rejected $c '缺少回传证据'
    $c = New-Case 'bad-transfer'; $c.Manifest.remove[0].archive.receivedSha256 = 'b' * 64; Expect-Rejected $c '回传哈希不匹配'
    $c = New-Case 'changed-source'; [IO.File]::WriteAllText((Join-Path $c.Root 'builds/task/old/source.txt'), 'changed'); Expect-Rejected $c '归档后源文件变化'
    $c = New-Case 'new-file'; [IO.File]::WriteAllText((Join-Path $c.Root 'builds/task/old/new.txt'), 'new'); Expect-Rejected $c '归档后新增文件'
    $c = New-Case 'traversal'; $c.Manifest.remove[0].batch = '../dependencies'; Expect-Rejected $c '批次越界'
    $c = New-Case 'overlap'; $c.Manifest.remove[0].batch = 'task/good'; Expect-Rejected $c '删除目标与保留批次重叠'
    $c = New-Case 'wrong-owner'; $c.Manifest.remove[0].projectId = 'other'; Expect-Rejected $c '项目归属不匹配'
    $c = New-Case 'bad-cache'; $c.Manifest.remove[0].excludedCacheDirectories = @('source'); Expect-Rejected $c '任意目录不能冒充缓存'

    $c = New-Case 'preflight-all'
    $c.Manifest.remove += [ordered]@{ batch = 'task/unknown'; projectId = 'other' }
    Expect-Rejected $c '后续目标无效时首个目标也不得删除'

    $c = New-Case 'parent-junction'
    $alias = Join-Path $suite 'linked-project'
    New-Item -ItemType Junction -Path $alias -Target $c.Root | Out-Null
    $c.Root = $alias; $c.Manifest.projectRoot = $alias
    try { Expect-Rejected $c '项目根目录链接' } finally { [IO.Directory]::Delete($alias) }

    $c = New-Case 'junction'
    New-Item -ItemType Junction -Path (Join-Path $c.Root 'builds/task/old/linked') -Target (Join-Path $c.Root 'dependencies') | Out-Null
    Expect-Rejected $c '批次包含目录链接'
    [IO.Directory]::Delete((Join-Path $c.Root 'builds/task/old/linked'))

    $c = New-Case 'cache'
    New-Item -ItemType Directory -Path (Join-Path $c.Root 'builds/task/old/cache') | Out-Null
    [IO.File]::WriteAllText((Join-Path $c.Root 'builds/task/old/cache/rebuildable'), 'cache')
    $c.Manifest.remove[0].excludedCacheDirectories = @('cache'); Invoke-Case $c -Execute
    Assert-True (!(Test-Path -LiteralPath (Join-Path $c.Root 'builds/task/old'))) '已明确排除的构建缓存可随旧批次删除'
    $script:passed++

    $c = New-Case 'file-names'
    foreach ($name in @('.metadata', '文件 说明.txt')) {
        $file = Join-Path $c.Root ('builds/task/old/' + $name)
        [IO.File]::WriteAllText($file, 'owned data')
        $c.Manifest.remove[0].files += [ordered]@{path=$name;length=(Get-Item -LiteralPath $file).Length;sha256=(Get-FileHash -LiteralPath $file).Hash}
    }
    Invoke-Case $c -Execute
    Assert-True (!(Test-Path -LiteralPath (Join-Path $c.Root 'builds/task/old'))) '合法隐藏文件和中文空格文件名应参与完整归档校验'
    $script:passed++

    $c = New-Case 'active-process'
    $sleep = Join-Path $c.Root 'builds/task/old/active.exe'
    Copy-Item -LiteralPath (Join-Path $env:SystemRoot 'System32/cmd.exe') -Destination $sleep
    $c.Manifest.remove[0].files += [ordered]@{path='active.exe';length=(Get-Item -LiteralPath $sleep).Length;sha256=(Get-FileHash -LiteralPath $sleep).Hash}
    $process = Start-Process -FilePath $sleep -ArgumentList '/c ping -n 20 127.0.0.1 > nul' -PassThru -WindowStyle Hidden
    try { Expect-Rejected $c '批次存在真实活动进程' } finally { Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue }
    Write-Host "PASS: $script:passed 个保留策略安全用例"
} finally {
    if (Test-Path -LiteralPath $suite) { Remove-Item -LiteralPath $suite -Recurse -Force }
}
