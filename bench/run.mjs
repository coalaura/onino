import { mkdirSync, readFileSync, writeFileSync, existsSync, readdirSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { availableParallelism } from "node:os";
import { collect, hash } from "./system.mjs";
import { trial } from "./trial.mjs";
import { publish, csv } from "./report.mjs";

export const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const options = { runs: 5, warmup: 15, seconds: 20, resume: "", onino: "", baseline: "", replace: "" },
	args = process.argv.slice(2);

for (let index = 0; index < args.length; index++) {
	const name = args[index].replace(/^--/, "");

	if (name === "help") {
		console.log("bun bench/run.mjs [--onino PATH] [--runs 5] [--warmup 15] [--seconds 20] [--resume measurements/bench-ID] [--baseline measurements/bench-ID --replace CONFIGURATION]\nRequires manually downloaded official mkp224o, Onionloom and Prefix32 executables under bin/. Builds onino with builder (PACE, CGO, gpu); --onino supplies an existing build instead. Runs all modes and workloads, validates counters/modes/saved files, and updates .github/benchmarks.csv, comparison SVGs and README. Logs and saved keys stay under ignored measurements/. Resume continues a stopped suite without rerunning completed attempts. Baseline/replace reruns one configuration in a new suite and retains all other baseline samples verbatim.");

		process.exit(0);
	}

	if (!(name in options) || args[index + 1] === undefined) {
		throw new Error(`Unknown or incomplete option: ${args[index]}`);
	}

	options[name] = typeof options[name] === "number" ? Number(args[++index]) : args[++index];
}

if (!Number.isInteger(options.runs) || options.runs < 5 || !Number.isFinite(options.warmup) || options.warmup < 15 || !Number.isFinite(options.seconds) || options.seconds < 20) {
	throw new Error("Require at least five runs, 15 seconds warmup and 20 seconds target measurement.");
}

if (Boolean(options.baseline) !== Boolean(options.replace) || (options.resume && options.baseline)) {
	throw new Error("Use --baseline and --replace together, or --resume alone.");
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

const configurations = [
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

const workloads = ["somethingrare", "hello", "privacy", "donate+mirror+secure"],
	rows = [];

let baseline;

if (manifest.options.baseline) {
	const source = resolve(root, manifest.options.baseline),
		prior = JSON.parse(readFileSync(join(source, "manifest.json"), "utf8"));

	if (!configurations.some(config => config.id === manifest.options.replace)) {
		throw new Error(`Unknown replacement configuration: ${manifest.options.replace}`);
	}

	if (prior.threads !== manifest.threads || ["cpu", "nvidia", "platform", "release"].some(key => prior.hardware[key] !== manifest.hardware[key]) || ["runs", "warmup", "seconds"].some(key => prior.options[key] !== manifest.options[key])) {
		throw new Error("Replacement suite requires matching hardware, driver, workers and measurement settings.");
	}

	baseline = new Map(readdirSync(source).filter(name => /^\d.*\.json$/.test(name)).map(name => {
		const row = JSON.parse(readFileSync(join(source, name), "utf8"));

		return [`${row.round}-${row.configuration}-${row.workload}`, row];
	}));

	if (baseline.size !== manifest.options.runs * configurations.length * workloads.length) {
		throw new Error("Baseline must contain a complete suite of attempts.");
	}

	manifest.replacement = { configuration: manifest.options.replace, baseline: source, manifest_sha256: hash(join(source, "manifest.json")), samples_sha256: hash(join(source, "samples.csv")) };
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
			} else if (baseline && configuration.id !== manifest.options.replace) {
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

publish(root, rows, manifest, configurations, workloads);

console.log(`Published ${rows.length} attempts to .github/benchmarks.csv. Full transcripts and keys: ${directory}`);
