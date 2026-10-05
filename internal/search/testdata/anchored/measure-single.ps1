param(
	[Parameter(Mandatory = $true)][string]$Baseline,
	[Parameter(Mandatory = $true)][string]$Candidate,
	[string]$Name = "single",
	[string]$Bench = "^BenchmarkFullSearch$",
	[string]$Time = "2s",
	[int]$Count = 5,
	[long]$Affinity = 4
)

$ErrorActionPreference = "Stop"
$env:GOMAXPROCS = "1"
$env:GOAMD64 = "v1"
$process = Get-Process -Id $PID
$previous = $process.ProcessorAffinity
$executables = @((Resolve-Path $Baseline).Path, (Resolve-Path $Candidate).Path)
$labels = @("before", "after")

try {
	$process.ProcessorAffinity = $Affinity
	foreach ($executable in $executables) {
		& $executable '-test.run=^$' "-test.bench=$Bench" '-test.benchtime=1s' '-test.count=1' '-test.cpu=1' | Out-Null
		if ($LASTEXITCODE -ne 0) {
			throw "Warm-up failed: $executable"
		}
	}

	for ($sample = 0; $sample -lt $Count; $sample++) {
		$order = @(0, 1)
		if ($sample % 2) {
			$order = @(1, 0)
		}

		foreach ($index in $order) {
			$output = & $executables[$index] '-test.run=^$' "-test.bench=$Bench" '-test.benchmem' "-test.benchtime=$Time" '-test.count=1' '-test.cpu=1'
			if ($LASTEXITCODE -ne 0) {
				throw "Benchmark failed: $($executables[$index])"
			}

			$file = "measurements/$Name-$($labels[$index]).txt"
			if ($sample -eq 0) {
				$output | Set-Content -Encoding UTF8 $file
			} else {
				$output | Add-Content -Encoding UTF8 $file
			}

			$output
		}
	}
} finally {
	$process.ProcessorAffinity = $previous
}
