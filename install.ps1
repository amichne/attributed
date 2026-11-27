[CmdletBinding()]
param(
    [string]$Version = $(if ($env:GIT_ATTRIBUTED_VERSION) { $env:GIT_ATTRIBUTED_VERSION } else { "latest" }),
    [string]$BinDir = $(if ($env:GIT_ATTRIBUTED_BIN_DIR) { $env:GIT_ATTRIBUTED_BIN_DIR } else { Join-Path $HOME "bin" }),
    [switch]$NoGitHook,
    [switch]$NoAgentHooks,
    [switch]$GitTemplate
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repository = "https://github.com/amichne/attributed"
function Install-GitTemplateHooks {
    param([string]$Target)
    if (-not $GitTemplate) {
        return
    }
    & $Target install-hooks --agent none --git --template
    if ($LASTEXITCODE -ne 0) {
        throw "Could not install Git template hooks"
    }
}

if ($Version -eq "latest") {
    $release = Invoke-RestMethod `
        -Uri "https://api.github.com/repos/amichne/attributed/releases/latest" `
        -Headers @{ Accept = "application/vnd.github+json" }
    $Version = [string]$release.tag_name
}
if ($Version -notmatch "^v[0-9]+\.[0-9]+\.[0-9]+$") {
    throw "Unsupported release version: $Version"
}

# Re-running the installer is the update path: skip the download entirely
# when the installed binary already reports the target release.
$target = Join-Path $BinDir "git-attributed.exe"
$previous = $null
if (Test-Path -LiteralPath $target -PathType Leaf) {
    $current = try { & $target version 2>$null } catch { $null }
    if ($current -eq "git-attributed $Version") {
        Install-GitTemplateHooks -Target $target
        Write-Output "git-attributed $Version is already installed at $target"
        return
    }
    if ($current -match "^git-attributed v[0-9]+\.[0-9]+\.[0-9]+$") {
        $previous = $current.Substring("git-attributed ".Length)
    }
}

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
switch ($architecture) {
    "X64" { $arch = "amd64" }
    "Arm64" { $arch = "arm64" }
    default { throw "Unsupported architecture: $architecture" }
}

$releaseVersion = $Version.Substring(1)
$archive = "git-attributed_${releaseVersion}_windows_${arch}.zip"
$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("git-attributed-" + [guid]::NewGuid())
$archivePath = Join-Path $temporary $archive
$checksumsPath = Join-Path $temporary "checksums.txt"
$binaryPath = Join-Path $temporary "git-attributed.exe"
$staged = $null

New-Item -ItemType Directory -Path $temporary | Out-Null
try {
    Invoke-WebRequest -Uri "$repository/releases/download/$Version/$archive" -OutFile $archivePath
    Invoke-WebRequest -Uri "$repository/releases/download/$Version/checksums.txt" -OutFile $checksumsPath

    $escapedArchive = [regex]::Escape($archive)
    $checksumLines = @(Get-Content -LiteralPath $checksumsPath | Where-Object {
        $_ -match "^([0-9a-fA-F]{64})\s+\*?$escapedArchive$"
    })
    if ($checksumLines.Count -ne 1) {
        throw "Missing or duplicate checksum for $archive"
    }
    $null = $checksumLines[0] -match "^([0-9a-fA-F]{64})"
    $expected = $Matches[1].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Checksum mismatch for $archive"
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $archiveFiles = @(
            $zip.Entries |
                Where-Object { -not $_.FullName.EndsWith("/") } |
                ForEach-Object { $_.FullName.TrimStart([char[]]"./") } |
                Sort-Object
        )
        $expectedFiles = @("LICENSE", "README.md", "SECURITY.md", "git-attributed.exe") | Sort-Object
        if (Compare-Object -ReferenceObject $expectedFiles -DifferenceObject $archiveFiles) {
            throw "Unexpected files in $archive"
        }
        $entries = @($zip.Entries | Where-Object { $_.FullName -eq "git-attributed.exe" })
        if ($entries.Count -ne 1 -or $entries[0].Length -eq 0) {
            throw "Archive must contain exactly one git-attributed.exe"
        }
        $source = $entries[0].Open()
        try {
            $destination = [System.IO.File]::Create($binaryPath)
            try {
                $source.CopyTo($destination)
            }
            finally {
                $destination.Dispose()
            }
        }
        finally {
            $source.Dispose()
        }
    }
    finally {
        $zip.Dispose()
    }

    $reportedVersion = & $binaryPath version
    if ($reportedVersion -ne "git-attributed $Version") {
        throw "Binary version does not match release"
    }

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    if (Test-Path -LiteralPath $target -PathType Container) {
        throw "Destination is a directory: $target"
    }
    $staged = Join-Path $BinDir (".git-attributed-" + [guid]::NewGuid() + ".exe")
    Copy-Item -LiteralPath $binaryPath -Destination $staged
    Move-Item -LiteralPath $staged -Destination $target -Force

    if (-not $NoGitHook -and
        (Get-Command git -ErrorAction SilentlyContinue) -and
        (git rev-parse --is-inside-work-tree 2>$null) -eq "true") {
        & $target install-hooks --agent none --git --project
    }

    # -GitTemplate manages Git hooks in the git-attributed Git template
    # directory, so every new git init and git clone is attributed.
    Install-GitTemplateHooks -Target $target

    if ($previous) {
        Write-Output "Updated git-attributed $previous to $Version at $target"
    }
    else {
        Write-Output "Installed git-attributed $Version to $target"
    }

    if (-not $NoAgentHooks) {
        # Detection reads a command name and a configuration directory. It
        # never writes anything.
        function Test-Agent {
            param([string]$Command, [string]$ConfigDirectory)
            if (Get-Command $Command -ErrorAction SilentlyContinue) { return $true }
            if ($ConfigDirectory -and
                (Test-Path -LiteralPath (Join-Path $HOME $ConfigDirectory) -PathType Container)) {
                return $true
            }
            return $false
        }

        Write-Output ""
        # Factory and Claude Code hooks are installed at user level, so they
        # cover every repository on this machine.
        foreach ($agent in @(
            @{ Name = "droid"; Command = "droid"; Directory = ".factory" },
            @{ Name = "claude"; Command = "claude"; Directory = ".claude" }
        )) {
            if (Test-Agent -Command $agent.Command -ConfigDirectory $agent.Directory) {
                & $target install-hooks --agent $agent.Name --user | Out-Null
                if ($LASTEXITCODE -eq 0) {
                    Write-Output "Installed the $($agent.Name) hook for every repository."
                }
                else {
                    Write-Warning "Could not install the $($agent.Name) hook. Run: git-attributed install-hooks --agent $($agent.Name) --user"
                }
            }
        }

        # The remaining agents read a project hook or plugin file that git-attributed does
        # not own, so report the exact copy command instead of guessing a
        # user-level path.
        $pending = @(
            @{ Name = "gemini"; Command = "gemini"; Directory = ".gemini"; Source = "gemini"; Hook = ".gemini/settings.json"; SourceFile = "settings.json" },
            @{ Name = "cursor"; Command = "cursor"; Directory = ".cursor"; Source = "cursor"; Hook = ".cursor/hooks.json"; SourceFile = "hooks.json" },
            @{ Name = "codex"; Command = "codex"; Directory = ".codex"; Source = "codex"; Hook = ".codex/hooks.json"; SourceFile = "hooks.json" },
            @{ Name = "windsurf"; Command = "windsurf"; Directory = ".codeium"; Source = "windsurf"; Hook = ".windsurf/hooks.json"; SourceFile = "hooks.json" },
            @{ Name = "copilot"; Command = "code"; Directory = ".vscode"; Source = "copilot"; Hook = ".github/hooks/promptscript.json"; SourceFile = "hooks.json" },
            @{ Name = "grok"; Command = "grok"; Directory = ".grok"; Source = "grok"; Hook = ".grok/hooks/promptscript.json"; SourceFile = "hooks.json" }
        ) | Where-Object { Test-Agent -Command $_.Command -ConfigDirectory $_.Directory }
        if ($pending) {
            Write-Output "Detected agents that need one project integration file:"
            foreach ($agent in $pending) {
                $directory = Split-Path -Parent $agent.Hook
                Write-Output ("  {0,-14} New-Item -ItemType Directory -Force -Path '{4}' | Out-Null; irm {1}/raw/main/marketplace/harness/{2}/{3} -OutFile '{5}'" -f
                    $agent.Name, $repository, $agent.Source, $agent.SourceFile, $directory, $agent.Hook)
            }
            Write-Output "Merge the block for gemini instead of replacing the file."
            Write-Output "Details: $repository/blob/main/marketplace/harness/README.md"
        }
    }

    if (($env:PATH -split [System.IO.Path]::PathSeparator) -notcontains $BinDir) {
        Write-Output "Add $BinDir to PATH before starting an AI agent."
    }
}
finally {
    if ($staged) {
        Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $temporary -Recurse -Force -ErrorAction SilentlyContinue
}
