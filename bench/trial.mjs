import { mkdirSync, writeFileSync, readFileSync, readdirSync, existsSync } from "node:fs";
import { join, basename } from "node:path";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { cache } from "./system.mjs";

const scales = { "": 1, k: 1e3, K: 1e3, M: 1e6, G: 1e9, B: 1e9, T: 1e12, P: 1e15, E: 1e18 };

export function counter(tool, line) {
	if (tool === "mkp224o") {
		const match = line.match(/calc\/sec:([\d.]+).*elapsed:([\d.]+)sec/);
		if (match) {
			const rate = Number(match[1]),
				elapsed = Number(match[2]);

			return {
				count: rate * elapsed,
				error: (rate + elapsed) * 0.0000005,
				basis: "reconstructed cumulative calc/sec (-T) * internal elapsed"
			};
		}
	} else if (tool === "onionloom") {
		const match = line.match(/Tested ([\d,]+) possible addresses/);

		if (match) {
			return {
				count: Number(match[1].replaceAll(",", "")),
				error: 0,
				basis: "integer cumulative possible addresses"
			};
		}
	} else {
		const pattern = tool === "onino" ? /\| ([\d.]+)([kKMGBTPE]?) checked \|/ : /\| ([\d.]+)([kKMGBTPE]?) keys \|.*ETA/,
			match = line.match(pattern);

		if (match && !/tuning|initializing|starting/.test(line)) {
			const scale = scales[match[2]],
				decimals = match[1].includes(".") ? match[1].split(".")[1].length : 0;

			return {
				count: Number(match[1]) * scale,
				error: scale * 10 ** -decimals,
				basis: "rounded cumulative counter; conservative one displayed unit endpoint error"
			};
		}
	}

	return null;
}

function invocation(tool, config, prefixes, keys, threads) {
	const gpu = config.id.endsWith("gpu") || config.id.endsWith("combined");

	if (tool === "mkp224o") {
		return ["-t", String(config.workers), "-S", "2", "-T", "-d", keys, ...prefixes];
	}

	if (tool === "onionloom") {
		return ["search", "--workers", String(config.workers), "--gpu", gpu ? "force" : "off", "--continuous", "--output", keys, ...prefixes];
	}

	if (tool === "onino") {
		return ["--cpu", config.workers ? String(config.workers) : "off", ...(gpu ? ["--gpu", "auto"] : []), "--output", keys, ...prefixes.map(prefix => `${prefix}.`)];
	}

	return ["--no-print", ...(gpu ? ["--gpu", "--gpu-effort", "10"] : ["--threads", String(threads), "--count", "0"]), ...prefixes];
}

function modeValid(config, setup) {
	const text = setup.join("\n"),
		gpu = config.id.endsWith("gpu") || config.id.endsWith("combined");

	if (config.tool === "mkp224o") {
		return text.includes(`using ${config.workers} threads`);
	}

	if (config.tool === "onionloom") {
		return text.includes(`Chosen CPU setup: ${config.workers} parallel search tasks`) && (gpu ? /GPU setting: .*enabled .*beside the CPU/.test(text) : text.includes("GPU setting: off"));
	}

	if (config.tool === "onino") {
		const cpu = config.workers ? text.includes(`${config.workers} worker(s)`) : /CPU\s+disabled/.test(text);

		return cpu && (gpu ? /\d+ streams .*\d+ rounds/.test(text) && !/GPU\s+disabled/.test(text) : /GPU\s+disabled/.test(text));
	}

	return gpu ? /Device:.*OpenCL/.test(text) && /Dispatch:/.test(text) : text.includes(`CPU (${config.workers} threads)`);
}

export function inspectKeys(directory, prefixes) {
	let complete = 0,
		incomplete = 0,
		invalid = 0,
		files = 0;

	function visit(path) {
		const entries = readdirSync(path, { withFileTypes: true });

		files += entries.filter(entry => entry.isFile()).length;

		if (entries.some(entry => entry.name === "hostname" || entry.name === "hs_ed25519_secret_key")) {
			const names = ["hostname", "hs_ed25519_public_key", "hs_ed25519_secret_key"];

			if (!names.every(name => existsSync(join(path, name)))) {
				incomplete++;

				return;
			}

			const hostname = readFileSync(join(path, "hostname"), "utf8").trim(),
				publicKey = readFileSync(join(path, names[1])),
				secretKey = readFileSync(join(path, names[2]));

			if (!hostname || publicKey.length !== 64 || secretKey.length !== 96) {
				incomplete++;

				return;
			}

			const checksum = createHash("sha3-256").update(".onion checksum").update(publicKey.subarray(32)).update(Buffer.from([3])).digest(),
				bytes = Buffer.concat([publicKey.subarray(32), checksum.subarray(0, 2), Buffer.from([3])]);

			let encoded = "",
				buffer = 0,
				bits = 0;

			for (const byte of bytes) {
				buffer = (buffer << 8) | byte;
				bits += 8;

				while (bits >= 5) {
					bits -= 5;

					encoded = `${encoded}${"abcdefghijklmnopqrstuvwxyz234567"[(buffer >>> bits) & 31]}`;
				}
			}

			const headersValid = publicKey.subarray(0, 32).toString() === "== ed25519v1-public: type0 ==\0\0\0" && secretKey.subarray(0, 32).toString() === "== ed25519v1-secret: type0 ==\0\0\0";

			if (!headersValid || hostname.replace(/\.onion$/, "") !== encoded || !prefixes.some(prefix => encoded.startsWith(prefix))) {
				invalid++;
			} else if (basename(path) !== hostname) {
				incomplete++;
			} else {
				complete++;
			}
		}

		for (const entry of entries) {
			if (entry.isDirectory()) {
				visit(join(path, entry.name));
			}
		}
	}

	visit(directory);

	return {
		complete: complete,
		incomplete: incomplete,
		invalid: invalid,
		files: files
	};
}

export async function trial(manifest, config, workload, round, order, directory) {
	const tool = manifest.tools[config.artifact || config.tool],
		prefixes = workload.split("+"),
		keys = join(directory, "keys");

	mkdirSync(keys, { recursive: true });

	const args = invocation(config.tool, config, prefixes, keys, manifest.threads);

	const settings = {
		experiment: manifest.options.experiment || "",
		screen: Boolean(manifest.options.screen),
		warmup_seconds: manifest.options.warmup,
		measurement_seconds: manifest.options.seconds,
		rotation: "round/workload cyclic configuration rotation; workloads rotate each round",
		runner_sha256: manifest.runner,
		keys: "normal saving enabled",
		termination: manifest.termination,
		startup_timeout_seconds: 600,
		search_timeout_seconds: manifest.options.warmup + manifest.options.seconds + 120
	};

	const row = {
		suite_utc: manifest.started_utc,
		started_utc: new Date().toISOString(),
		round: round,
		order: order,
		tool: config.tool,
		configuration: config.id,
		execution_model: config.mode,
		workload: workload,
		search_workers: config.workers,
		available_threads: manifest.threads,
		status: "",
		notes: "",
		diagnostic: "",
		million_per_second: "",
		elapsed_s: "",
		warmup_s: "",
		first_search_s: "",
		start_s: "",
		end_s: "",
		start_counter: "",
		end_counter: "",
		delta: "",
		counter_error_bound_mps: "",
		counter_basis: "",
		start_line: "",
		end_line: "",
		progress_reports: 0,
		saved_keys: 0,
		incomplete_keys: 0,
		invalid_keys: 0,
		key_files: 0,
		exit_code: "",
		signal: "",
		command: [tool.path, ...args],
		cwd: keys,
		version: tool.version,
		binary_sha256: tool.sha256,
		build: tool,
		hardware: manifest.hardware,
		runtime: manifest.runtime,
		environment: manifest.environment,
		settings: settings,
		selected_setup: [],
		cache_before: config.tool === "prefix32" && tool.path ? cache(tool.path) : "",
		cache_after: ""
	};

	if (!tool.path) {
		row.status = "missing_binary";
		row.notes = tool.build;

		return row;
	}

	const origin = process.hrtime.bigint(),
		elapsed = () => Number(process.hrtime.bigint() - origin) / 1e9;

	const transcript = [],
		snapshots = [],
		setup = [];

	let first,
		start,
		end,
		failure = "",
		stopping = false,
		deadline = 600,
		killTimer;

	const child = spawn(tool.path, args, {
		cwd: keys,
		stdio: ["ignore", "pipe", "pipe"],
		windowsHide: true
	});

	function stop() {
		if (stopping) {
			return;
		}

		stopping = true;

		child.kill("SIGINT");

		killTimer = setTimeout(() => child.kill("SIGKILL"), 5000);
	}

	function lineReceived(raw, time) {
		const line = raw.replace(/\x1b\[[0-9;]*m/g, "").trim();

		if (!line || stopping) {
			return;
		}

		if (/^(using |in total,|CPU setting:|Chosen CPU|Paired candidate|Measured CPU|GPU setting:|Search setup|Patterns\s|CPU\s|Features\s|Build\s|Engine\s|Matching\s|GPU\s|Startup\s|\d+ streams|configuration selected|Device:|Tuning:|Dispatch:|Prefix:)/.test(line)) {
			setup.push(line);
		}

		const sample = counter(config.tool, line);

		if (!sample || sample.count <= 0) {
			return;
		}

		if (!modeValid(config, setup)) {
			failure = "requested_mode_not_confirmed";

			stop();

			return;
		}

		sample.time = time;
		sample.line = line;

		const previous = snapshots.at(-1);

		if (previous && sample.count < previous.count) {
			failure = "counter_regression";

			stop();

			return;
		}

		snapshots.push(sample);

		if (!first) {
			first = sample;

			deadline = time + manifest.options.warmup + manifest.options.seconds + 120;
		}

		if (!start && time - first.time >= manifest.options.warmup) {
			start = sample;
		} else if (start && time - start.time >= manifest.options.seconds - 0.05) {
			end = sample;

			stop();
		}
	}

	for (const [name, stream] of [["stdout", child.stdout], ["stderr", child.stderr]]) {
		let pending = "";

		stream.setEncoding("utf8");

		stream.on("data", text => {
			const time = elapsed();

			transcript.push({
				seconds: time,
				stream: name,
				text: text
			});

			pending = `${pending}${text}`;

			const parts = pending.split(/[\r\n]/);

			pending = parts.pop();

			for (const part of parts) {
				lineReceived(part, time);
			}

			// Prefix32 flushes CR-prefixed progress without a newline; timestamp it now, not at the next dispatch.
			if (config.tool === "prefix32" && / keys \|.*ETA.* {6}$/.test(pending)) {
				lineReceived(pending, time);

				pending = "";
			}
		});
	}

	const timeout = setInterval(() => {
		if (elapsed() > deadline) {
			failure = first ? "search_timeout" : "startup_or_mode_timeout";

			stop();
		}
	}, 1000);

	let interrupted = false;

	const interrupt = () => {
		interrupted = true;

		stop();
	};

	process.once("SIGINT", interrupt);

	await new Promise(resolve => {
		child.on("error", error => {
			failure = error.message;
		});

		child.on("close", (code, signal) => {
			row.exit_code = code;
			row.signal = signal;

			resolve();
		});
	});

	clearInterval(timeout);
	clearTimeout(killTimer);

	process.removeListener("SIGINT", interrupt);

	writeFileSync(join(directory, "transcript.json"), JSON.stringify(transcript));
	writeFileSync(join(directory, "counters.json"), JSON.stringify(snapshots));

	row.selected_setup = setup;
	row.progress_reports = snapshots.length;
	row.first_search_s = first?.time ?? "";
	row.start_s = start?.time ?? "";
	row.end_s = end?.time ?? "";
	row.start_counter = start?.count ?? "";
	row.end_counter = end?.count ?? "";
	row.start_line = start?.line ?? "";
	row.end_line = end?.line ?? "";
	row.counter_basis = start?.basis ?? first?.basis ?? "";
	row.warmup_s = start ? start.time - first.time : "";
	row.cache_after = config.tool === "prefix32" ? cache(tool.path) : "";

	const inventory = inspectKeys(keys, prefixes);

	row.saved_keys = inventory.complete;
	row.incomplete_keys = inventory.incomplete;
	row.invalid_keys = inventory.invalid;
	row.key_files = inventory.files;
	row.diagnostic = transcript.map(chunk => chunk.text).join("").split(/[\r\n]/).map(line => line.trim()).filter(line => /^(Error:|error:|GPU init failed:|fatal:|Failed)/.test(line)).slice(0, 5).join("\n");
	row.status = failure ? "invalid" : end ? "ok" : row.exit_code === 0 ? "early_exit" : "process_error";
	row.notes = failure || (end ? "" : "No full post-warmup window; see diagnostic and transcript.json for exit/capability details.");

	if (config.id === "prefix32-cpu" && workload !== "somethingrare") {
		row.status = end ? "incomparable_stop_on_match" : row.status;
		row.notes = "CPU search stops on first match despite --count 0; no sustained shorter-workload estimate (avoids survivor bias).";
	}

	if (inventory.invalid || (workload === "somethingrare" && inventory.files)) {
		row.status = "invalid";
		row.notes = "Invalid saved key or unexpected rare-prefix match; inspect keys.";
	}

	if (inventory.incomplete) {
		row.notes = `${row.notes} ${inventory.incomplete} incomplete saves at termination.`.trim();
	}

	if (end) {
		row.elapsed_s = end.time - start.time;
		row.delta = end.count - start.count;
		row.counter_error_bound_mps = (end.error + start.error) / row.elapsed_s / 1e6;

		if (row.delta <= end.error + start.error) {
			row.status = "invalid";
			row.notes = "Counter delta does not exceed display precision.";
		}

		if (row.status === "ok") {
			row.million_per_second = row.delta / row.elapsed_s / 1e6;
		}
	}

	if (interrupted) {
		row.status = "interrupted";
	}

	return row;
}
