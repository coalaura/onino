param(
	[string]$Baseline = "measurements/anchored-baseline-measure.exe",
	[string]$Candidate = "measurements/anchored-scalar-measure.exe",
	[string]$Name = "anchored-workers",
	[string]$Workloads = "donate,privacy,rare,three6,three7,512",
	[string]$Workers = "1,16,32",
	[int]$Seconds = 3,
	[int]$Pairs = 6
)

$ErrorActionPreference = "Stop"
$env:GOAMD64 = "v1"
$env:GODEBUG = "cpu.avx512f=off,cpu.avx512bw=off,cpu.avx512vl=off"
$env:ONINO_MEASURE = $Workloads
$env:ONINO_WORKERS = $Workers
$env:ONINO_PLACEMENTS = "spread"
$env:ONINO_MODES = "parallel"
$env:ONINO_SECONDS = "$Seconds"
$env:ONINO_REPEATS = "1"

for ($pair = 0; $pair -lt $Pairs; $pair++) {
	$order = @(0, 1)
	if ($pair % 2 -ne 0) {
		$order = @(1, 0)
	}

	foreach ($index in $order) {
		$binary = @($Baseline, $Candidate)[$index]
		$side = @("before", "after")[$index]
		& $binary '-test.run=^TestMeasureMulticore$' '-test.v' '-test.timeout=30m' | Tee-Object -FilePath "measurements/$Name-$pair-$side.txt"
		if ($LASTEXITCODE -ne 0) {
			throw "Measurement failed: $LASTEXITCODE"
		}
	}
}
