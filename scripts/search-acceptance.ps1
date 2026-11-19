param(
    [ValidateSet('Prepare','Seed','Baseline','Candidate','Plans','All')][string]$Stage = 'Prepare',
    [string]$Artifacts = 'C:/Users/saber/.thinkrail/search-experience-research/acceptance-05',
    [string]$Go = 'C:/Users/saber/scoop/apps/go/current/bin/go.exe'
)
$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path "$PSScriptRoot/..").Path.Replace('\','/')
$Evidence = "$Root/.scratch/search-experience/acceptance"
$Baseline = "$Artifacts/baseline"
$SHA = '86c510d6501c0525c2e66617718522699176ff30'
$env:TEMP = $env:TMP = $env:TMPDIR = $env:GOTMPDIR = 'C:/tmp'
$env:GOCACHE = 'C:/Users/saber/AppData/Local/go-build'
New-Item -ItemType Directory -Force $Artifacts,$Evidence | Out-Null
Set-Location $Root
function Invoke-Checked([scriptblock]$Command) {
    & $Command
    if ($LASTEXITCODE -ne 0) { throw "Command failed ($LASTEXITCODE): $Command" }
}
function Prepare {
    # Never use an active implementation tree as a baseline. No archived source
    # is edited: the one shared measurement test is added virtually by overlay.
    if (!(Test-Path $Baseline)) {
        Invoke-Checked { git archive "--output=$Artifacts/baseline.tar" $SHA }
        New-Item -ItemType Directory $Baseline | Out-Null
        Invoke-Checked { tar -xf "$Artifacts/baseline.tar" -C $Baseline }
    }
    $replace = @{}
    $replace["$Baseline/internal/api/search_acceptance_test.go"] = "$Root/internal/api/search_acceptance_test.go"
    @{Replace=$replace} | ConvertTo-Json -Depth 4 | Set-Content "$Artifacts/overlay.json" -Encoding ASCII
    Push-Location $Baseline
    try { Invoke-Checked { & $Go test -overlay "$Artifacts/overlay.json" -c -o "$Artifacts/baseline-api.test.exe" ./internal/api } } finally { Pop-Location }
    Invoke-Checked { & $Go test -c -o "$Artifacts/candidate-api.test.exe" ./internal/api }
}
if ($Stage -in @('Prepare','All')) { Prepare }
if ($Stage -in @('Seed','All')) {
    $env:SEARCH_ACCEPTANCE_SEED = "$Artifacts/frozen-v4.db"
    # Existing mother databases are rejected, never overwritten or migrated.
    Invoke-Checked { & "$Artifacts/baseline-api.test.exe" '-test.run=^TestSearchAcceptanceSeed$' -test.v '-test.timeout=45m' }
    Remove-Item Env:SEARCH_ACCEPTANCE_SEED
    Get-FileHash "$Artifacts/frozen-v4.db" -Algorithm SHA256
}
if ($Stage -in @('Baseline','Candidate','All')) {
    $labels = @($Stage.ToLower())
    if ($Stage -eq 'All') { $labels = @('baseline','candidate') }
    $env:SEARCH_ACCEPTANCE_INPUT = "$Artifacts/frozen-v4.db"
    foreach ($label in $labels) {
        $env:SEARCH_ACCEPTANCE_LABEL = $label
        $env:SEARCH_ACCEPTANCE_OUTPUT = "$Evidence/$label-replay.json"
        Invoke-Checked { & "$Artifacts/$label-api.test.exe" '-test.run=^TestSearchAcceptanceMeasure$' -test.v '-test.timeout=90m' }
    }
}
if ($Stage -in @('Plans','All')) {
    Copy-Item "$Artifacts/frozen-v4.db" "$Artifacts/plan-replay.db"
    $env:SEARCH_ACCEPTANCE_PLAN_DB = "$Artifacts/plan-replay.db"
    Invoke-Checked { & $Go test ./internal/database '-run=^TestSearchAcceptanceQueryPlan$' -v -count=1 }
}
