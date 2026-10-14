import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export function csv(rows) {
	const fields = Object.keys(rows[0]),
		quote = value => `"${(typeof value === "object" && value !== null ? JSON.stringify(value) : String(value ?? "")).replaceAll('"', '""')}"`;

	return `${fields.join(",")}\n${rows.map(row => fields.map(field => quote(row[field])).join(",")).join("\n")}\n`;
}

function summary(rows, configuration, workload, count) {
	const samples = rows.filter(row => row.configuration === configuration && row.workload === workload && row.status === "ok").map(row => Number(row.million_per_second)).sort((left, right) => left - right);

	if (samples.length !== count) {
		return null;
	}

	const middle = Math.floor(samples.length / 2);

	return {
		median: samples.length % 2 ? samples[middle] : (samples[middle - 1] + samples[middle]) / 2,
		min: samples[0],
		max: samples.at(-1)
	};
}

function format(value) {
	return value.toLocaleString("en-US", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
}

function escape(value) {
	return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;");
}

function svg(series, manifest, graphics, light) {
	const foreground = light ? "#27242d" : "#f3eedf",
		muted = light ? "#645e70" : "#b7afc3";

	const maximum = Math.max(1, ...series.map(item => item.stats?.max ?? 0)),
		limit = Math.ceil(maximum / 2000) * 2000,
		width = 550,
		scale = value => value / limit * width;

	const parts = [
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1100 650" role="img" aria-labelledby="title desc">`,
		`<title id="title">Sustained somethingrare throughput</title>`,
		`<desc id="desc">${manifest.options.runs} runs per configuration; median bars and min-max whiskers. Million candidates per second, zero-based scale. GPU execution models differ as labeled.</desc>`,
		`<g font-family="Segoe UI,Arial,sans-serif" fill="${foreground}">`,
		`<text x="30" y="40" font-size="24" font-weight="700">somethingrare · sustained search</text>`,
		`<text x="30" y="67" font-size="14" fill="${muted}">${escape(manifest.hardware.cpu.trim())} · ${manifest.threads} available CPU threads</text>`,
		`<text x="30" y="90" font-size="14" fill="${muted}">${escape(graphics)}</text>`
	];

	for (let tick = 0; tick <= limit; tick += limit / 4) {
		const position = 340 + scale(tick);

		parts.push(`<path d="M${position} 120V565" stroke="${muted}" opacity="0.18"/><text x="${position}" y="590" text-anchor="middle" font-size="13" fill="${muted}">${format(tick)}</text>`);
	}

	series.forEach((item, index) => {
		const position = 143 + index * 47;

		parts.push(`<text x="30" y="${position}" font-size="16">${escape(item.label)}</text>`);

		if (item.stats) {
			const stats = item.stats;

			parts.push(`<rect x="340" y="${position - 19}" width="${scale(stats.median)}" height="27" rx="3" fill="${light ? "#8060a5" : "#b18ad5"}"/>`, `<path d="M${340 + scale(stats.min)} ${position - 5}H${340 + scale(stats.max)}M${340 + scale(stats.min)} ${position - 12}V${position + 2}M${340 + scale(stats.max)} ${position - 12}V${position + 2}" stroke="${foreground}" stroke-width="2"/>`, `<text x="1060" y="${position}" text-anchor="end" font-size="16">${format(stats.median)}</text>`);
		} else {
			parts.push(`<text x="355" y="${position}" font-size="14" fill="${muted}">unavailable / incomplete</text>`);
		}
	});

	parts.push(`<text x="30" y="624" font-size="14" fill="${muted}">M candidates/s · median and min–max · ${manifest.options.runs} runs · ≥${manifest.options.warmup}s search warmup · ~${manifest.options.seconds}s windows</text>`, `</g></svg>`);

	return `${parts.join("\n")}\n`;
}

export function publish(root, rows, manifest, configurations, workloads) {
	writeFileSync(join(root, ".github", "benchmarks.csv"), csv(rows));

	const labels = ["mkp224o CPU", "Onionloom CPU", "Onionloom GPU + 1 CPU worker", "Onionloom CPU+GPU", "Onino CPU", "Onino GPU-only", "Onino CPU+GPU", "Prefix32 CPU", "Prefix32 GPU-accelerated"],
		series = configurations.map((config, index) => ({ label: labels[index], stats: summary(rows, config.id, "somethingrare", manifest.options.runs) })),
		selectedGPU = rows.flatMap(row => row.selected_setup).find(line => /^GPU\s{2,}(?!disabled)/.test(line)),
		graphics = manifest.hardware.nvidia.startsWith("unavailable:") ? selectedGPU ?? "GPU unavailable; see dataset" : manifest.hardware.nvidia.split(",").slice(0, 2).join(" · driver");

	for (const light of [false, true]) {
		writeFileSync(join(root, ".github", `prefix-comparison${light ? "-light" : ""}.svg`), svg(series, manifest, graphics, light));
	}

	const table = configurations.map((config, index) => {
		const cells = workloads.map(workload => {
			const stats = summary(rows, config.id, workload, manifest.options.runs),
				valid = rows.filter(row => row.configuration === config.id && row.workload === workload && row.status === "ok").length;

			return stats ? format(stats.median) : config.id === "prefix32-cpu" && workload !== "somethingrare" ? "stops on match" : `incomplete (${valid}/${manifest.options.runs})`;
		});

		return `| ${labels[index]} | ${cells.join(" | ")} |`;
	});

	const rareKeys = rows.filter(row => row.workload === "somethingrare").reduce((total, row) => total + row.key_files, 0),
		publishingErrors = rows.some(row => row.tool === "onionloom" && /failed to atomically install key directory.*Access is denied/.test(row.diagnostic)),
		failureNote = publishingErrors ? " Onionloom's incomplete cells include Windows key-publishing failures in the official binary; failed attempts remain in the CSV." : "",
		tick = "`";

	const text = `## Benchmarks

Measured ${manifest.started_utc.slice(0, 10)} on **${manifest.hardware.cpu.trim()}**, **${manifest.threads} available CPU threads**, **${manifest.hardware.platform} ${manifest.hardware.release}**, **${graphics}**. Values are median **million candidates/second** from ${manifest.options.runs} runs; higher is better. The primary ${tick}somethingrare${tick} workload produced ${rareKeys} key files.

| Configuration | ${tick}somethingrare${tick} | ${tick}hello${tick} | ${tick}privacy${tick} | ${tick}donate${tick}, ${tick}mirror${tick}, ${tick}secure${tick} |
| --- | ---: | ---: | ---: | ---: |
${table.join("\n")}

<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/prefix-comparison.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/prefix-comparison-light.svg">
	<img alt="Sustained somethingrare throughput: all nine configurations, median and min-max, on a shared zero-based scale." src=".github/prefix-comparison-light.svg">
</picture>

**Method.** Sequential runs rotate configuration and workload order. Each warms up for ≥${manifest.options.warmup}s after the first observed post-setup search counter, then measures a ~${manifest.options.seconds}s progress-bounded window. Candidate-counter deltas use external monotonic wall time, excluding startup/autotuning. Onino and Prefix32 counters are rounded; mkp224o's cumulative count is reconstructed from its ${tick}-T${tick} average × elapsed time. Precision bounds are recorded. Normal key saving remains enabled and saved prefixes/checksums are checked. Shutdown/save drainage is outside measurement (forced termination on Windows); this measures search throughput, not completed-key delivery.

**Modes and limits.** CPU and CPU+GPU use all available search workers. Onionloom GPU-assisted retains **one CPU search worker**. Onino GPU-only uses host coordination without CPU search. Prefix32 GPU-accelerated uses CPU assistance, **not independent CPU search**. Prefix32 CPU stops on its first match, so shorter workloads have no sustained estimate; surviving runs are not selectively averaged. Its fixed ${tick}prefix32p${tick} ${tick}--bench${tick} is not substituted. Multiple prefixes mean OR. Exits, missing capabilities and invalid samples are explicit; medians require all runs to be valid.${failureNote}

**Reproduce:** manually extract unmodified official [mkp224o](https://github.com/cathugger/mkp224o/releases), [Onionloom](https://github.com/chrisch88dev/onionloom/releases) and [Prefix32](https://github.com/0xROOTPLS/Prefix32/releases) releases under ignored ${tick}bin/${tick}, keeping one executable per tool and its shipped libraries. With Bun and the build prerequisites above installed, run **${tick}bun bench/run.mjs${tick}**. It builds onino with builder/PACE, GPU enabled and PGO off, runs all checks, and regenerates this section, the CSV and both SVG themes. ${tick}--onino PATH${tick} accepts an existing build; ${tick}--resume measurements/bench-ID${tick} continues a stopped suite. Logs and real keys stay in ignored ${tick}measurements/${tick}; allow about two hours. Prefix32's own tuning cache is reused and recorded.

The [consolidated CSV](.github/benchmarks.csv) contains every sample, exact versions/hashes, commands, build details, hardware and selected runtime settings. Versions: ${manifest.tools.mkp224o.version}, ${manifest.tools.onionloom.version.split("\n")[0]}, ${manifest.tools.prefix32.version}; onino ${tick}${manifest.tools.onino.version}${tick}. Unreported competitor compiler flags remain unknown. Earlier [Linux summaries](.github/linux-rare-prefix.csv) and [progress reports](.github/linux-rare-prefix-progress.csv) remain historical records with different workloads/timing.

`;

	const path = join(root, "README.md"),
		readme = readFileSync(path, "utf8"),
		begin = readme.indexOf("## Benchmarks\n"),
		end = readme.indexOf("## Optimization history\n", begin);

	if (begin < 0 || end < 0) {
		throw new Error("README benchmark boundaries missing");
	}

	writeFileSync(path, `${readme.slice(0, begin)}${text}${readme.slice(end)}`);
}
