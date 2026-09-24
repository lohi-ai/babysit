# preamble.ps1 — the single shared PowerShell preamble for babysit skills.
#
# Mirrors the bash block in preamble.md: same state-echo keys, same session
# files, same telemetry rows. Skills on native Windows (PowerShell/pwsh, no
# bash on PATH) dot-source or invoke this once at start, and once at end:
#
#   pwsh -NoProfile -File preamble.ps1 -SkillName <name>
#   pwsh -NoProfile -File preamble.ps1 -Phase end -SkillName <name> `
#       -SessionId <id from SESSION_ID echo> -Outcome success `
#       -TelStart <epoch from TEL_START echo> -Telemetry <v from TELEMETRY echo>
#
# The echo contract is load-bearing: skills parse TICKET:/SLUG:/SPAWNED: etc.
# Keep keys byte-identical with the bash block — drift silently breaks every
# skill on Windows.
[CmdletBinding()]
param(
    [string]$SkillName = 'SKILL_NAME',
    [ValidateSet('start', 'end')]
    [string]$Phase = 'start',
    [string]$SessionId = '',
    [string]$Outcome = 'unknown',
    [long]$TelStart = 0,
    [string]$Telemetry = ''
)

$ErrorActionPreference = 'Continue'

function Invoke-Bbs {
    # bbs is a native binary; a missing binary or non-zero exit must not
    # terminate the preamble (bash `|| true` / `2>/dev/null` parity).
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Args)
    try {
        $out = & bbs @Args 2>$null
        if ($LASTEXITCODE -ne 0) { return $null }
        return ($out -join "`n")
    } catch { return $null }
}

$Home_ = $HOME
if (-not $Home_) { $Home_ = [Environment]::GetFolderPath('UserProfile') }
$SessDir = Join-Path $Home_ '.babysit/sessions'

if ($Phase -eq 'end') {
    # ── Telemetry end + session-file cleanup (bash "run last" block) ──
    # bash removes ~/.babysit/sessions/$PPID — the file the start phase
    # touched under the parent pid, not the session id.
    $_PPID = $PID
    try { $_PPID = (Get-Process -Id $PID -ErrorAction Stop).Parent.Id } catch { }
    Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $SessDir "$_PPID")
    if ($SessionId -eq '') { $SessionId = "$PID" }
    if ($Telemetry -ne 'off') {
        $dur = 0
        if ($TelStart -gt 0) { $dur = [long](Get-Date -UFormat %s) - $TelStart }
        $ts = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
        $row = '{{"ts":"{0}","skill":"{1}","event":"end","session":"{2}","duration_s":{3},"outcome":"{4}"}}' -f $ts, $SkillName, $SessionId, $dur, $Outcome
        New-Item -ItemType Directory -Force -ErrorAction SilentlyContinue (Join-Path $Home_ '.babysit/analytics') | Out-Null
        try { [IO.File]::AppendAllText((Join-Path $Home_ '.babysit/analytics/skill-usage.jsonl'), $row + "`n") } catch { }
    }
    exit 0
}

# ── Skill preamble (start) ───────────────────────────────────────
$_SESSION_ID = "$PID-$([long](Get-Date -UFormat %s))"
$_TEL_START = [long](Get-Date -UFormat %s)

# ── Bin reachability ─────────────────────────────────────────────
# Install guarantees `bbs` on PATH (setup-skills.ps1 copies it to
# ~/.local/bin; brew installs it). Prepend the absolute install dirs for
# shells that don't inherit a login PATH — same net as the bash block.
foreach ($d in @((Join-Path $Home_ '.local/bin'), (Join-Path $Home_ '.claude'),
                $(if ($env:CLAUDE_PLUGIN_ROOT) { Join-Path $env:CLAUDE_PLUGIN_ROOT 'bin' } else { $null }),
                (Join-Path $Home_ '.claude/skills/babysit/bin'))) {
    if ($d -and (Test-Path $d) -and ($env:PATH -split [IO.Path]::PathSeparator) -notcontains $d) {
        $env:PATH = "$d$([IO.Path]::PathSeparator)$env:PATH"
    }
}

# Capability probe, once — `bbs ticket --help` exits 0 only when the binary
# actually serves the subcommand (a stale binary exits 1 silently). Invoke-Bbs
# returns $null on any failure; --help always prints on success.
if ($null -eq (Invoke-Bbs ticket --help)) {
    [Console]::Error.WriteLine('BBS_DEGRADED: no working `bbs` on PATH — run bin/setup-skills.ps1 from a checkout, or `brew install lohi-ai/babysit/bbs` (a plugin install ships no compiled binary)')
}

# Auto-update check — prints UPGRADE_AVAILABLE/JUST_UPGRADED to stderr.
$_UPD = Invoke-Bbs update check
if ($_UPD) { [Console]::Error.WriteLine($_UPD) }

# ── Session tracking — count concurrent sessions, prune stale (>120 min) ──
# Portable sweep: no `find` — on Windows, C:\Windows\System32\find.exe shadows
# the POSIX find and silently eats these calls (audit bs-b3m7rnkw #9).
New-Item -ItemType Directory -Force -ErrorAction SilentlyContinue $SessDir | Out-Null
$_PPID = $PID
try { $_PPID = (Get-Process -Id $PID -ErrorAction Stop).Parent.Id } catch { }
New-Item -ItemType File -Force -ErrorAction SilentlyContinue (Join-Path $SessDir "$_PPID") | Out-Null
$_SESSIONS = 0
$_cutoff = (Get-Date).AddMinutes(-120)
Get-ChildItem -Force -File -ErrorAction SilentlyContinue $SessDir | ForEach-Object {
    if ($_.LastWriteTime -gt $_cutoff) { $_SESSIONS++ }
    else { Remove-Item -Force -ErrorAction SilentlyContinue $_.FullName }
}

# ── Session-writer — persist ~/.babysit/sessions/<id>.yaml ───────
$_BABYSIT_SESSION = $env:BABYSIT_SESSION
if (-not $_BABYSIT_SESSION) {
    if ($env:CLAUDE_CODE_SESSION_ID) { $_BABYSIT_SESSION = "cc-$($env:CLAUDE_CODE_SESSION_ID)" }
    elseif ($env:CODEX_SESSION_ID) { $_BABYSIT_SESSION = "cx-$($env:CODEX_SESSION_ID)" }
}
if ($_BABYSIT_SESSION) {
    $_SF = Join-Path $SessDir "$_BABYSIT_SESSION.yaml"
    $_now = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    $_started = "started_at: $_now"
    if (Test-Path $_SF) {
        $m = Get-Content $_SF -ErrorAction SilentlyContinue | Where-Object { $_ -like 'started_at:*' } | Select-Object -First 1
        if ($m) { $_started = $m }
    }
    $_tmp = Join-Path $SessDir (".session." + [Guid]::NewGuid().ToString('N'))
    # WriteAllLines is BOM-free UTF-8 on every PowerShell version — Set-Content
    # would emit UTF-16LE on PS5.1 and a BOM on PS7, corrupting the YAML.
    try {
        [IO.File]::WriteAllLines($_tmp, @(
            'version: 1'
            "session_id: $_BABYSIT_SESSION"
            "ticket: $($env:BABYSIT_TICKET)"
            $_started
            "last_seen_at: $_now"
            "pid: $PID"
            "cwd: $((Get-Location).Path)"
        ))
    } catch { }
    Move-Item -Force -ErrorAction SilentlyContinue $_tmp $_SF
}

# ── Config + repo state ──────────────────────────────────────────
$_PROACTIVE = Invoke-Bbs config get proactive; if (-not $_PROACTIVE) { $_PROACTIVE = 'true' }
$_TEL = Invoke-Bbs config get telemetry; if (-not $_TEL) { $_TEL = 'local' }
$_BRANCH = try { (git branch --show-current 2>$null) } catch { $null }
if (-not $_BRANCH) { $_BRANCH = 'unknown' }
$_top = try { (git rev-parse --show-toplevel 2>$null) } catch { $null }
$_REPO = if ($_top) { Split-Path -Leaf $_top } else { 'unknown' }
$_INVOKER = if ($env:AGENT_ROLE) { $env:AGENT_ROLE } elseif ($env:GT_ROLE) { $env:GT_ROLE } else { 'developer' }
$_AGENT = $env:BABYSIT_AGENT
if (-not $_AGENT -and $env:CODEX_SESSION_ID) { $_AGENT = 'codex' }
if (-not $_AGENT -and ($env:GROK_SESSION_ID -or $env:GROK_AGENT)) { $_AGENT = 'grok' }
if (-not $_AGENT -and $env:CLAUDE_CODE_SESSION_ID) { $_AGENT = 'claude' }
$_SKILL_REF = switch ($_AGENT) { 'codex' { '$bbs:' } 'omp' { '/' } default { '/bbs:' } }
$_SPAWNED = if ($env:OPENCLAW_SESSION) { 'true' } else { 'false' }

# ── Project scope — identity ladder via `bbs ticket env` ─────────
# ticket env prints POSIX single-quoted KEY='value' lines; parse them here
# instead of eval (PowerShell has none). Values may contain Windows paths.
$SLUG = 'unknown'; $TICKET = ''; $BABYSIT_PROJECT_HOME = ''
$_env = Invoke-Bbs ticket env
if ($_env) {
    foreach ($line in ($_env -split "`n")) {
        if ($line -match "^([A-Z_]+)='(.*)'$") {
            $v = $Matches[2] -replace "'\\''", "'"
            switch ($Matches[1]) {
                'SLUG' { $SLUG = $v }
                'TICKET' { $TICKET = $v }
                'BABYSIT_PROJECT_HOME' { $BABYSIT_PROJECT_HOME = $v }
            }
        }
    }
}
if (-not $BABYSIT_PROJECT_HOME) {
    $BABYSIT_PROJECT_HOME = if ($env:BABYSIT_PROJECT_HOME) { $env:BABYSIT_PROJECT_HOME } else { Join-Path $Home_ ".babysit/projects/$SLUG" }
}

Write-Output "SKILL: $SkillName"
Write-Output "SESSION_ID: $_SESSION_ID"
Write-Output "SESSIONS_ACTIVE: $_SESSIONS"
Write-Output "SLUG: $SLUG"
Write-Output "BRANCH: $_BRANCH"
Write-Output "REPO: $_REPO"
Write-Output "INVOKER: $_INVOKER"
Write-Output "AGENT: $(if ($_AGENT) { $_AGENT } else { 'unknown' })"
Write-Output "SKILL_REF: $_SKILL_REF"
Write-Output "TICKET: $(if ($TICKET) { $TICKET } else { '<none>' })"
Write-Output "PROJECT_HOME: $BABYSIT_PROJECT_HOME"
Write-Output "PROACTIVE: $_PROACTIVE"
Write-Output "TELEMETRY: $_TEL"
Write-Output "SPAWNED: $_SPAWNED"
Write-Output "TEL_START: $_TEL_START"

# Ticket folder — idempotent.
if ($TICKET) { $null = Invoke-Bbs ticket init }

# Context Recovery — v2 snapshot when the installed binary advertises it.
$_V2_SNAPSHOT = Invoke-Bbs autopilot snapshot --json
$_v2ok = $false
if ($_V2_SNAPSHOT) {
    try {
        $_snap = $_V2_SNAPSHOT | ConvertFrom-Json -ErrorAction Stop
        if ($_snap.schema_version -eq 2 -and $_snap.ok -eq $true) {
            $_v2ok = $true
            Write-Output 'AUTOPILOT_CONTRACT: v2'
            $d = $_snap.data
            $packet = [ordered]@{
                snapshot_id = $d.snapshot_id; state_revision = $d.state_revision
                ticket = $d.ticket; run = $d.run; git = $d.git
                policy = $d.policy; gates = $d.gates; obligations = $d.obligations
            }
            Write-Output ($packet | ConvertTo-Json -Compress -Depth 6)
        }
    } catch { }
}
if (-not $_v2ok -and $TICKET) { $null = Invoke-Bbs autopilot recover }

# Record skill start as JSONL (local-only, unless telemetry=off).
if ($_TEL -ne 'off') {
    $ts = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    $row = '{{"ts":"{0}","skill":"{1}","event":"start","session":"{2}","repo":"{3}","branch":"{4}","invoker":"{5}"}}' -f $ts, $SkillName, $_SESSION_ID, $_REPO, $_BRANCH, $_INVOKER
    New-Item -ItemType Directory -Force -ErrorAction SilentlyContinue (Join-Path $Home_ '.babysit/analytics') | Out-Null
    try { [IO.File]::AppendAllText((Join-Path $Home_ '.babysit/analytics/skill-usage.jsonl'), $row + "`n") } catch { }
}
