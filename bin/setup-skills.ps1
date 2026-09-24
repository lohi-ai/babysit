# setup-skills.ps1 — Babysit companion-bin setup for native Windows
# (PowerShell 5.1+/pwsh). Mirror of bin/setup-skills: builds bbs, installs the
# bbs-* bins, links bbs onto PATH, validates skills, installs the pre-commit
# hook. Skills themselves are managed by the agent's plugin system.
#
#   pwsh -NoProfile -File bin/setup-skills.ps1 [-DryRun] [-Full] [-Uninstall]
#
# Windows note: symlinks need Developer Mode or elevation, so bins are
# symlinked when possible and copied otherwise — a copied bbs.exe dispatches
# on argv[0] exactly like the symlinked multicall binary.
[CmdletBinding()]
param(
    [switch]$DryRun,
    [switch]$Full,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Continue'
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$ProjectSkills = Join-Path $ProjectDir '.claude/skills'

$Home_ = $HOME
if (-not $Home_) { $Home_ = [Environment]::GetFolderPath('UserProfile') }
$GlobalClaude = Join-Path $Home_ '.claude'
$GlobalSkills = Join-Path $GlobalClaude 'skills'
$CodexSkills = Join-Path $Home_ '.codex/skills'
$LocalBin = Join-Path $Home_ '.local/bin'

function info($m) { Write-Host "→ $m" -ForegroundColor Blue }
function ok($m) { Write-Host "✓ $m" -ForegroundColor Green }
function warn($m) { Write-Host "! $m" -ForegroundColor Yellow }
function fail($m) { Write-Host "✗ $m" -ForegroundColor Red; exit 1 }

function Invoke-Step {
    param([scriptblock]$Action, [string]$What)
    if ($DryRun) { Write-Host "  [dry-run] $What" } else { & $Action }
}

# The bins every install links: bbs plus the bbs-* argv0 aliases.
$Bins = @('bbs', 'bbs-config', 'bbs-dashboard', 'bbs-design', 'bbs-env',
          'bbs-autopilot', 'bbs-slug', 'bbs-ticket', 'bbs-qa-config',
          'bbs-secrets', 'bbs-update-check', 'bbs-update', 'bbs-upgrade')

if ($Uninstall) {
    info 'Uninstalling Babysit bins...'
    foreach ($bin in ($Bins + @('babysit', 'bbs-formula', 'bbs-pipeline', 'bbs-run',
                               'bbs-learnings-log', 'bbs-learnings-search',
                               'bbs-telemetry-log', 'bbs-codex-competitive', 'bbs-analytics-cron'))) {
        foreach ($dst in @((Join-Path $GlobalClaude $bin), (Join-Path $GlobalClaude "$bin.exe"),
                           (Join-Path $LocalBin $bin), (Join-Path $LocalBin "$bin.exe"))) {
            if (Test-Path $dst) { Invoke-Step { Remove-Item -Force $dst } "rm $dst"; ok "Removed $dst" }
        }
    }
    $hook = Join-Path $ProjectDir '.git/hooks/pre-commit'
    $hookSrc = Join-Path $ScriptDir 'hooks/pre-commit'
    if ((Test-Path $hook) -and (Select-String -Quiet -Path $hook -Pattern 'Babysit workflow lint' -ErrorAction SilentlyContinue)) {
        Invoke-Step { Remove-Item -Force $hook } "rm $hook"
        ok 'Removed pre-commit hook'
    }
    Write-Host "`nUninstall complete." -ForegroundColor Green
    Write-Host 'Skills installed via a plugin system must be uninstalled in that agent.'
    exit 0
}

# ─── Step 1: Validate project structure ───────────────────────
info 'Validating project structure...'
if (-not (Test-Path $ProjectSkills)) { fail '.claude/skills/ not found — run from project root' }
if (-not (Test-Path (Join-Path $ProjectDir '.claude-plugin/plugin.json'))) { warn '.claude-plugin/plugin.json missing — plugin install won''t work' }
$skillCount = @(Get-ChildItem -Directory $ProjectSkills | Where-Object { $_.Name -notin @('references', 'shared') -and -not $_.Name.StartsWith('.') }).Count
ok "Found $skillCount skills in project (managed by plugin system — see plugin.json)"
New-Item -ItemType Directory -Force $GlobalClaude | Out-Null

# ─── Step 1b: Build the bbs Go CLI ────────────────────────────
info 'Building bbs Go CLI...'
$bbsExe = Join-Path $ProjectDir 'bin/bbs.exe'
$bbsBin = Join-Path $ProjectDir 'bin/bbs'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    warn 'go not found — skipping bbs build (install Go to enable the bbs CLI and its bbs-* bins)'
} elseif ($DryRun) {
    Write-Host '  [dry-run] go build -o bin/bbs.exe ./cmd/bbs'
} else {
    Push-Location $ProjectDir
    try {
        # Build bbs.exe explicitly: on Windows `go build -o bin/bbs` can emit an
        # extension-less binary nothing can exec; keep both names like the
        # bash installer so POSIX callers and native callers both resolve.
        & go build -o bin/bbs.exe ./cmd/bbs
        if ($LASTEXITCODE -eq 0) {
            if (-not (Test-Path $bbsBin)) { Copy-Item $bbsExe $bbsBin }
            ok 'bbs built → bin/bbs.exe'
        } else {
            warn 'go build failed — the bbs-* bins will not resolve until built'
        }
    } finally { Pop-Location }
}

# ─── Step 2: Install babysit bins ─────────────────────────────
info 'Installing babysit bins...'
$installed = @()

function Install-Bin {
    param([string]$Name, [string]$DstDir = $GlobalClaude)
    # On Windows the multicall binary must carry .exe; on POSIX the bare name.
    $exe = $IsWindows -or $env:OS -eq 'Windows_NT'
    $src = if ($exe) { $bbsExe } else { $bbsBin }
    $dst = Join-Path $DstDir ($Name + $(if ($exe) { '.exe' } else { '' }))
    if (-not (Test-Path $src)) { warn "$Name not installed — bin/bbs not built"; return }
    if (Test-Path $dst) {
        # Same content → up-to-date; otherwise replace.
        $same = (Get-FileHash $src -ErrorAction SilentlyContinue).Hash -eq (Get-FileHash $dst -ErrorAction SilentlyContinue).Hash
        if ($same) { ok "$Name up-to-date"; $script:installed += $Name; return }
        Invoke-Step { Remove-Item -Force $dst } "rm $dst"
        warn "$Name replaced"
    }
    # Prefer a symlink (Developer Mode / elevated); fall back to a copy —
    # bbs.exe dispatches on argv[0], so a copied binary is a working alias.
    $linked = $false
    if (-not $DryRun) {
        try { New-Item -ItemType SymbolicLink -Path $dst -Target $src -ErrorAction Stop | Out-Null; $linked = $true } catch { }
    }
    if ($linked) { ok "$Name → $dst" }
    else { Invoke-Step { Copy-Item $src $dst } "copy $src → $dst"; ok "$Name → $dst (copy)" }
    $script:installed += $Name
}

foreach ($bin in $Bins) { Install-Bin $bin }

# ─── Step 2b: Put bbs on PATH (~/.local/bin) ──────────────────
info 'Linking bbs onto PATH...'
New-Item -ItemType Directory -Force $LocalBin | Out-Null
Install-Bin 'bbs' $LocalBin
if (($env:PATH -split [IO.Path]::PathSeparator) -notcontains $LocalBin) {
    warn "$LocalBin is not on your PATH — add it to run `bbs` from a shell"
}

# ─── Step 3: Validate skill integrity ─────────────────────────
info 'Validating skills...'
$errors = 0
foreach ($dir in (Get-ChildItem -Directory $ProjectSkills | Where-Object { $_.Name -notin @('references', 'shared') -and -not $_.Name.StartsWith('.') })) {
    if (-not (Test-Path (Join-Path $dir.FullName 'SKILL.md'))) { warn "$($dir.Name): missing SKILL.md"; $errors++ }
}
foreach ($ref in @('preamble.md', 'preamble.ps1', 'auto-decision-framework.md', 'handoff-contracts.md')) {
    if (-not (Test-Path (Join-Path $ProjectSkills "references/$ref"))) { warn "references/$ref missing"; $errors++ }
}
if ($errors -eq 0) { ok 'All skills valid' } else { warn "$errors validation issue(s) found" }

# ─── Step 4: Install pre-commit hook ──────────────────────────
# Git for Windows ships Git-Bash, so the hook stays a bash script on every OS.
info 'Installing pre-commit hook...'
$hookSrc = Join-Path $ScriptDir 'hooks/pre-commit'
$hookDst = Join-Path $ProjectDir '.git/hooks/pre-commit'
if (-not (Test-Path $hookSrc)) { warn 'bin/hooks/pre-commit not found — skipping hook install' }
elseif (-not (Test-Path (Join-Path $ProjectDir '.git/hooks'))) { warn '.git/hooks/ not found — skipping hook install' }
elseif (Test-Path $hookDst) {
    if (Select-String -Quiet -Path $hookDst -Pattern 'Babysit workflow lint' -ErrorAction SilentlyContinue) {
        ok 'pre-commit hook already invokes babysit lint'
    } else {
        Invoke-Step {
            Add-Content -Path $hookDst -Value "`n# ── Babysit workflow lint ────────────────────────────`nif [ -f `"$hookSrc`" ]; then`n  '$hookSrc' || exit 1`nfi"
        } "append babysit lint to $hookDst"
        ok 'pre-commit hook appended (babysit workflow lint)'
    }
} else {
    Invoke-Step { Copy-Item $hookSrc $hookDst } "copy $hookSrc → $hookDst"
    ok 'pre-commit hook → .git/hooks/pre-commit'
}

# ── Post-install (-Full) ───────────────────────────────────────
if ($Full) {
    Write-Host ''
    Write-Host 'Post-install: register the plugin' -ForegroundColor Cyan
    Write-Host 'Claude Code:'; Write-Host ''
    Write-Host "  /plugin marketplace add $ProjectDir"
    Write-Host '  /plugin install bbs@babysit'
    Write-Host ''
    Write-Host 'Codex CLI:'; Write-Host ''
    Write-Host "  codex plugin marketplace add $ProjectDir"
    Write-Host '  codex plugin add bbs@babysit'
    Write-Host ''
    Write-Host 'Grok Build:'
    Write-Host '  grok plugin install https://github.com/lohi-ai/babysit'
    Write-Host ''
    Write-Host 'OMP hooks (skills are configured separately; see docs/operations.md):'
    Write-Host "  omp --extension `"$ProjectDir/hooks/omp.ts`""
    Write-Host ''
    Write-Host 'Hook prerequisite: bbs on PATH (or bbs in ~/.local/bin).'
}

# ─── Summary ──────────────────────────────────────────────────
Write-Host ''
Write-Host 'Babysit bins installed.' -ForegroundColor Green
Write-Host ''
Write-Host "Skills:      $skillCount in .claude/skills/ — register in Claude Code:"
Write-Host "               /plugin marketplace add $ProjectDir"
Write-Host '               /plugin install bbs@babysit'
Write-Host '             or Codex CLI:'
Write-Host "               codex plugin marketplace add $ProjectDir"
Write-Host '               codex plugin add bbs@babysit'
if ($installed.Count -gt 0) { Write-Host "Bins:        ~/.claude/{$($installed -join ',')}" } else { Write-Host 'Bins:        none linked' }
Write-Host 'References:  .claude/skills/references/'
Write-Host ''
Write-Host 'To uninstall: bin/setup-skills.ps1 -Uninstall'
