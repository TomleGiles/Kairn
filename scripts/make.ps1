<#
  Équivalent Windows des principales cibles du Makefile (sans make ni Docker).
    .\scripts\make.ps1 demo      # API de démonstration (mémoire) + services Python + web
    .\scripts\make.ps1 test      # tests Go, Python et provider Terraform
    .\scripts\make.ps1 lint      # gofmt, go vet, ruff, mypy, eslint, tsc
    .\scripts\make.ps1 gen       # OpenAPI + documentation des connecteurs + client TypeScript
    .\scripts\make.ps1 docs      # site de documentation (apps\web\public\docs)
    .\scripts\make.ps1 e2e       # Playwright sur l'instance de démo ($env:E2E_CHANNEL = "chrome" : navigateur local)
    .\scripts\make.ps1 build     # binaires dans bin\
#>
param([Parameter(Mandatory = $true)][ValidateSet("demo", "test", "lint", "gen", "docs", "e2e", "build")] [string]$Target)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Invoke-Checked([string]$File, [string[]]$Arguments, [string]$Dir = $root) {
  Push-Location $Dir
  try {
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$File $($Arguments -join ' ') a échoué ($LASTEXITCODE)" }
  } finally { Pop-Location }
}

switch ($Target) {
  "demo" {
    $env:KAIRN_MODE = "demo"
    $env:KAIRN_AI_URL = "http://localhost:8091"
    $env:KAIRN_ANALYTICS_URL = "http://localhost:8090"
    Start-Process -NoNewWindow go -ArgumentList "run", "./services/api"
    $py = @{ KAIRN_API_URL = "http://localhost:8080"; KAIRN_SERVICE_TOKEN = "demo-service-token" }
    foreach ($k in $py.Keys) { Set-Item "env:$k" $py[$k] }
    Start-Process -NoNewWindow python -ArgumentList "-m", "kairn_analytics.app" -WorkingDirectory "$root\services\analytics"
    Start-Process -NoNewWindow python -ArgumentList "-c", "from kairn_ai.app import main; main()" -WorkingDirectory "$root\services\ai"
    Invoke-Checked "npm" @("run", "dev") "$root\apps\web"
  }
  "test" {
    Invoke-Checked "go" @("test", "./...", "-count=1")
    Invoke-Checked "python" @("-m", "pytest", "-q") "$root\services\analytics"
    Invoke-Checked "python" @("-m", "pytest", "-q") "$root\services\ai"
    Invoke-Checked "go" @("test", "./...", "-count=1") "$root\terraform-provider"
  }
  "lint" {
    $unformatted = & gofmt -l agent cli connectors migrations pkg services
    if ($unformatted) { throw "Fichiers non formatés : $unformatted" }
    Invoke-Checked "go" @("vet", "./...")
    Invoke-Checked "python" @("-m", "ruff", "check", ".") "$root\services\analytics"
    Invoke-Checked "python" @("-m", "mypy", "kairn_analytics") "$root\services\analytics"
    Invoke-Checked "python" @("-m", "ruff", "check", ".") "$root\services\ai"
    Invoke-Checked "python" @("-m", "mypy", "kairn_ai", "tests") "$root\services\ai"
    Invoke-Checked "npm" @("run", "lint") "$root\apps\web"
    Invoke-Checked "npm" @("run", "typecheck") "$root\apps\web"
  }
  "gen" {
    Invoke-Checked "go" @("run", "./services/api/cmd/openapi", "docs/api")
    Invoke-Checked "go" @("run", "./services/api/cmd/docs", "docs/connectors")
    Invoke-Checked "npm" @("run", "gen") "$root\apps\web"
  }
  "docs" {
    Invoke-Checked "npm" @("ci", "--no-audit", "--no-fund") "$root\apps\docs"
    Invoke-Checked "npm" @("test") "$root\apps\docs"
    Invoke-Checked "node" @("build.mjs") "$root\apps\docs"
  }
  "e2e" {
    Invoke-Checked "npm" @("ci", "--no-audit", "--no-fund") "$root\test\e2e"
    Invoke-Checked "npx" @("playwright", "test") "$root\test\e2e"
  }
  "build" {
    New-Item -ItemType Directory -Force bin | Out-Null
    $targets = @{ "kairn-api" = "./services/api"; "kairn-ingest" = "./services/ingest"; "kairn-cost-engine" = "./services/cost-engine";
                  "kairn-notifier" = "./services/notifier"; "kairn" = "./cli"; "kairn-agent" = "./agent" }
    foreach ($bin in $targets.Keys) { Invoke-Checked "go" @("build", "-trimpath", "-o", "bin/$bin.exe", $targets[$bin]) }
  }
}
