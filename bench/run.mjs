import { mkdirSync, readFileSync, writeFileSync, existsSync, readdirSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { availableParallelism } from "node:os";
import { collect, hash } from "./system.mjs";
import { trial } from "./trial.mjs";
import { publish, csv, readSamples } from "./report.mjs";

export const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const options = { runs: 5, warmup: 15, seconds: 20, resume: "", onino: "", release: "", baseline: "", replace: "", only: "", compare: "", workloads: "", experiment: "", screen: 0, exact: 0, streams: 0, rounds: 0, diagnostics: 0 },
	args = process.argv.slice(2);

for (let index = 0; index < args.length; index++) {
	const name = args[index].replace(/^--/, "");

	if (name === "help") {
		console.log("Local experiments: --only onino-gpu,onino-combined,onino-cpu [--compare BASELINE_PATH] [--experiment E01] [--workloads somethingrare,hello,privacy,donate+mirror+secure]. Subsets write samples.csv only within their measurements/ suite; the public CSV and charts contain main comparisons only. --screen 1 permits short rejection screens (not confirmation results).");
		console.log("Release refresh: --onino PATH --release v0.4.1 --baseline .github/benchmarks.csv --replace onino-cpu,onino-gpu,onino-combined. Baseline accepts a complete suite directory or its CSV; --replace accepts comma-separated configurations.");
		console.log("bun bench/run.mjs [--onino PATH] [--runs 5] [--warmup 15] [--seconds 20] [--resume measurements/bench-ID] [--baseline measurements/bench-ID --replace CONFIGURATION[,CONFIGURATION]]\nRequires manually downloaded official mkp224o, Onionloom and Prefix32 executables under bin/. Builds onino with builder (PACE, CGO, gpu); --onino supplies an existing build instead. Runs all modes and workloads, validates counters/modes/saved files, and updates .github/benchmarks.csv, comparison SVGs, index.html and README. Logs and saved keys stay under ignored measurements/. Resume continues a stopped suite without rerunning completed attempts. Baseline/replace refreshes selected configurations in a complete comparison dataset.");

		process.exit(0);
	}

	if (!(name in options) || args[index + 1] === undefined) {
		throw new Error(`Unknown or incomplete option: ${args[index]}`);
	}

	options[name] = typeof options[name] === "number" ? Number(args[++index]) : args[++index];
}

if (![0, 1].includes(options.screen) || !Number.isInteger(options.runs) || options.runs < (options.screen ? 1 : 5) || !Number.isFinite(options.warmup) || options.warmup < (options.screen ? 5 : 15) || !Number.isFinite(options.seconds) || options.seconds < (options.screen ? 5 : 20)) {
	throw new Error("Require at least five runs, 15 seconds warmup and 20 seconds target measurement.");
}

if ((options.compare || options.workloads || options.experiment || options.screen || options.exact || options.streams || options.rounds || options.diagnostics) && !options.only || options.only && (options.baseline || options.replace)) {
	throw new Error("Local experiment options require --only and cannot use --baseline/--replace.");
}

if (![0, 1, 2].includes(options.exact) || ![0, 1].includes(options.diagnostics) || !Number.isInteger(options.streams) || options.streams < 0 || options.streams > 32768 || !Number.isInteger(options.rounds) || options.rounds < 0 || options.rounds > 64) {
	throw new Error("Require --exact 0 (rounded), 1 (exact), or 2 (rounded with snapshots), --diagnostics 0/1, --streams 0-32768 and --rounds 0-64; zero uses automatic selection.");
}

if (options.only && options.only.split(",").some(name => !["onino-cpu", "onino-gpu", "onino-combined"].includes(name))) {
	throw new Error("--only accepts onino-cpu,onino-gpu,onino-combined.");
}

if (Boolean(options.baseline) !== Boolean(options.replace) || (options.resume && options.baseline)) {
	throw new Error("Use --baseline and --replace together, or --resume alone.");
}

if (options.release && (!options.onino || !/^v\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(options.release))) {
	throw new Error("--release requires --onino and a release tag such as v0.4.1.");
}

const directory = options.resume ? resolve(root, options.resume) : join(root, "measurements", `bench-${new Date().toISOString().replace(/[:.]/g, "-")}`);

mkdirSync(directory, { recursive: true });

const manifestPath = join(directory, "manifest.json");

let manifest;

if (options.resume) {
	manifest = JSON.parse(readFileSync(manifestPath, "utf8"));

	for (const tool of Object.values(manifest.tools)) {
		if (tool.path && hash(tool.path) !== tool.sha256) {
			throw new Error(`Binary changed since suite began: ${tool.path}`);
		}
	}

	manifest.runner = Object.fromEntries(["run.mjs", "system.mjs", "trial.mjs", "report.mjs"].map(name => [name, hash(join(root, "bench", name))]));
} else {
	manifest = collect(root, directory, options, availableParallelism());

	writeFileSync(manifestPath, JSON.stringify(manifest, null, "\t"));
}

let configurations = [
	{ id: "mkp224o-cpu", tool: "mkp224o", mode: "CPU", workers: manifest.threads },
	{ id: "onionloom-cpu", tool: "onionloom", mode: "CPU", workers: manifest.threads },
	{ id: "onionloom-gpu", tool: "onionloom", mode: "GPU-assisted + 1 CPU search worker", workers: 1 },
	{ id: "onionloom-combined", tool: "onionloom", mode: "CPU+GPU", workers: manifest.threads },
	{ id: "onino-cpu", tool: "onino", mode: "CPU", workers: manifest.threads },
	{ id: "onino-gpu", tool: "onino", mode: "GPU-only (host coordination)", workers: 0 },
	{ id: "onino-combined", tool: "onino", mode: "CPU+GPU", workers: manifest.threads },
	{ id: "prefix32-cpu", tool: "prefix32", mode: "CPU", workers: manifest.threads },
	{ id: "prefix32-gpu", tool: "prefix32", mode: "GPU-accelerated (CPU assistance; no CPU search)", workers: 0 }
];

if (manifest.options.only) {
	configurations = configurations.filter(config => manifest.options.only.split(",").includes(config.id));

	if (manifest.options.compare) {
		configurations = configurations.flatMap(config => [{ ...config, id: `baseline-${config.id}`, artifact: "reference" }, { ...config, id: `${manifest.options.experiment || "candidate"}-${config.id}` }]);
	}
}

const workloads = manifest.options.workloads ? manifest.options.workloads.split(",") : ["somethingrare", "hello", "privacy", "donate+mirror+secure"],
	rows = [];

if (workloads.some(workload => !/^[a-z2-7]{1,51}(\+[a-z2-7]{1,51}){0,7}$/.test(workload))) {
	throw new Error("Invalid anchored-prefix workload.");
}

let baseline;

if (manifest.options.baseline) {
	const source = resolve(root, manifest.options.baseline),
		snapshot = join(directory, "baseline-samples.csv"),
		replacements = manifest.options.replace.split(","),
		fromCSV = source.endsWith(".csv"),
		prior = fromCSV ? null : JSON.parse(readFileSync(join(source, "manifest.json"), "utf8")),
		priorRows = existsSync(snapshot) ? readSamples(snapshot) : fromCSV ? readSamples(source) : readdirSync(source).filter(name => /^\d.*\.json$/.test(name)).map(name => JSON.parse(readFileSync(join(source, name), "utf8")));

	if (replacements.some(id => !configurations.some(config => config.id === id))) {
		throw new Error(`Unknown replacement configuration: ${manifest.options.replace}`);
	}

	const incompatible = priorRows.some(row => row.available_threads !== manifest.threads || ["cpu", "nvidia", "platform", "release"].some(key => row.hardware[key] !== manifest.hardware[key]) || row.settings.warmup_seconds !== manifest.options.warmup || row.settings.measurement_seconds !== manifest.options.seconds);

	if (incompatible || prior && prior.options.runs !== manifest.options.runs) {
		throw new Error("Replacement suite requires matching hardware, driver, workers and measurement settings.");
	}

	baseline = new Map(priorRows.map(row => [`${row.round}-${row.configuration}-${row.workload}`, row]));

	if (baseline.size !== manifest.options.runs * configurations.length * workloads.length) {
		throw new Error("Baseline must contain a complete suite of attempts.");
	}

	for (const config of configurations) {
		if (replacements.includes(config.id)) {
			continue;
		}

		const recorded = priorRows.find(row => row.configuration === config.id);

		manifest.tools[config.tool] ??= recorded.build;
	}

	if (!existsSync(snapshot)) {
		writeFileSync(snapshot, csv(priorRows));
	}

	manifest.replacement ??= { configuration: manifest.options.replace, baseline: source, manifest_sha256: fromCSV ? null : hash(join(source, "manifest.json")), samples_sha256: hash(fromCSV ? source : join(source, "samples.csv")) };
	writeFileSync(manifestPath, JSON.stringify(manifest, null, "\t"));
}

let order = 0;

console.log(`Suite: ${directory}\n${manifest.threads} available CPU threads; ${manifest.options.runs} rounds, ${manifest.options.warmup}s warmup, ~${manifest.options.seconds}s measurement. Normal key saving is enabled.`);

for (let round = 0; round < manifest.options.runs; round++) {
	for (let workloadIndex = 0; workloadIndex < workloads.length; workloadIndex++) {
		const workloadPosition = (workloadIndex + round) % workloads.length,
			workload = workloads[workloadPosition];

		for (let configurationIndex = 0; configurationIndex < configurations.length; configurationIndex++) {
			const configuration = configurations[(configurationIndex + round + workloadPosition) % configurations.length];

			order++;

			const name = `${String(order).padStart(3, "0")}-${configuration.id}-${workload}`,
				resultPath = join(directory, `${name}.json`);

			let row;

			if (existsSync(resultPath)) {
				row = JSON.parse(readFileSync(resultPath, "utf8"));
			} else if (baseline && !manifest.options.replace.split(",").includes(configuration.id)) {
				row = baseline.get(`${round + 1}-${configuration.id}-${workload}`);

				if (!row) {
					throw new Error(`Missing baseline sample: ${name}`);
				}

				writeFileSync(resultPath, JSON.stringify(row, null, "\t"));
			} else {
				console.log(`[${order}/${manifest.options.runs * workloads.length * configurations.length}] round ${round + 1} ${configuration.id} ${workload}`);

				const attempt = join(directory, name);

				if (existsSync(attempt) && readdirSync(attempt).length) {
					throw new Error(`Interrupted attempt retained at ${attempt}; move it aside before resuming so key output starts empty.`);
				}

				row = await trial(manifest, configuration, workload, round + 1, order, attempt);

				if (row.status === "interrupted") {
					throw new Error(`Suite interrupted; retained ${attempt}. Move the incomplete attempt aside before --resume.`);
				}

				writeFileSync(resultPath, JSON.stringify(row, null, "\t"));

				console.log(`  ${row.status}${row.million_per_second ? `: ${Number(row.million_per_second).toFixed(1)} M/s` : ""}; ${row.notes}`);
			}

			rows.push(row);

			writeFileSync(join(directory, "samples.csv"), csv(rows));
		}
	}
}

for (const tool of Object.values(manifest.tools)) {
	if (tool.path && hash(tool.path) !== tool.sha256) {
		throw new Error(`Binary changed during suite: ${tool.path}`);
	}
}

if (manifest.options.only) {
	console.log(`Recorded ${rows.length} local ${manifest.options.screen ? "screen" : "confirmation"} attempts in ${join(directory, "samples.csv")}.`);
} else {
	publish(root, rows, manifest, configurations, workloads);

	console.log(`Published ${rows.length} attempts to .github/benchmarks.csv. Full transcripts and keys: ${directory}`);
}
