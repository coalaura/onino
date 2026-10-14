import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join, resolve, basename, dirname } from "node:path";
import { createHash } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import os from "node:os";

export function hash(path) {
	return createHash("sha256").update(readFileSync(path)).digest("hex");
}

export function command(executable, args, cwd) {
	const result = spawnSync(executable, args, { cwd: cwd, encoding: "utf8", timeout: 120000, stdio: ["ignore", "pipe", "pipe"] });

	if (result.error || result.status !== 0) {
		return `unavailable: ${result.error?.message ?? result.stderr}`;
	}

	return `${result.stdout}${result.stderr}`.trim();
}

function files(directory) {
	if (!existsSync(directory)) {
		return [];
	}

	return readdirSync(directory, { withFileTypes: true }).flatMap(entry => entry.isDirectory() ? files(join(directory, entry.name)) : [join(directory, entry.name)]);
}

export function cache(path) {
	const cachePath = join(dirname(path), ".prefix32_gpu_tune.txt");

	return existsSync(cachePath) ? readFileSync(cachePath, "utf8").trim() : "absent";
}

export function collect(root, directory, options, threads) {
	const windows = process.platform === "win32",
		suffix = windows ? ".exe" : "",
		binaries = files(join(root, "bin")),
		tools = {},
		names = options.only ? [] : ["mkp224o", "onionloom", "prefix32"];

	for (const name of names) {
		const matches = binaries.filter(path => name === "prefix32" ? /^prefix32(?:-.+)?(?:\.exe)?$/.test(basename(path)) && !/\.(txt|zip|gz|md)$/.test(path) : basename(path) === `${name}${suffix}`);

		if (matches.length > 1) {
			throw new Error(`Keep exactly one official ${name} executable under bin/: ${matches.join(", ")}`);
		}

		const path = matches[0];

		if (!path) {
			throw new Error(`Download and extract an official ${name} release under bin/ first.`);
		}

		const versionArgs = name === "mkp224o" ? ["-V"] : name === "onionloom" ? ["version"] : ["--version"],
			libraries = files(dirname(path)).filter(file => /\.(dll|so|dylib)$/.test(file));

		tools[name] = {
			path: path,
			sha256: hash(path),
			version: command(path, versionArgs, root),
			build: "Manually supplied official release; upstream compiler version/flags not reported by binary. Artifact name and SHA-256 identify the exact build.",
			libraries: libraries.map(file => ({ name: basename(file), sha256: hash(file) }))
		};
	}

	const onino = options.onino ? resolve(root, options.onino) : join(directory, `onino${suffix}`),
		buildArgs = ["build", "go", "--cgo", "--pace", "--dyn", "--compat", "--no-gen", "-pgo=off", "-tags", "gpu", "--output", onino];

	if (!options.onino) {
		console.log("Building onino with builder (PACE, GPU enabled, PGO off)...");

		execFileSync("builder", buildArgs, { cwd: root, stdio: "inherit", timeout: 120000 });
	}

	const revision = command("git", ["rev-parse", "HEAD"], root),
		sourcePattern = /\.(go|s|c|h|comp|spv)$|^go\.(mod|sum)$/,
		tracked = command("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], root).split("\0").filter(path => sourcePattern.test(path)).sort(),
		modified = command("git", ["diff", "HEAD", "--name-only", "-z"], root).split("\0"),
		untracked = command("git", ["ls-files", "--others", "--exclude-standard", "-z"], root).split("\0"),
		changes = [...new Set([...modified, ...untracked])].filter(path => sourcePattern.test(path)).sort(),
		sourceHash = createHash("sha256");

	for (const path of tracked) {
		const absolute = join(root, path);

		sourceHash.update(path).update("\0").update(existsSync(absolute) ? readFileSync(absolute) : "deleted").update("\0");
	}

	tools.onino = {
		path: onino,
		sha256: hash(onino),
		version: options.onino ? "user-supplied; see embedded build information" : `source ${revision}${changes.length ? " + working-tree changes" : ""}`,
		build: command("go", ["version", "-m", onino], root),
		build_command: options.onino ? "User-supplied binary; compiler invocation not known" : JSON.stringify(["builder", ...buildArgs]),
		source_revision: options.onino ? "unknown (user-supplied binary)" : revision,
		source_sha256: options.onino ? "unknown (user-supplied binary)" : sourceHash.digest("hex"),
		source_changes: options.onino ? [] : changes.map(path => ({ path: path, sha256: existsSync(join(root, path)) ? hash(join(root, path)) : "deleted" }))
	};

	if (options.compare) {
		const reference = resolve(root, options.compare);

		tools.reference = {
			path: reference,
			sha256: hash(reference),
			version: "preserved local Onino baseline; see embedded build information",
			build: command("go", ["version", "-m", reference], root),
			build_command: "User-supplied baseline; see research build record"
		};
	}

	const platform = { platform: process.platform, release: os.release(), architecture: os.arch(), cpu: os.cpus()[0]?.model, logical_cpus: os.cpus().length, available_threads: threads, memory_bytes: os.totalmem() };

	if (windows) {
		platform.details = command("powershell.exe", ["-NoProfile", "-Command", "$ErrorActionPreference='Stop'; [ordered]@{cpu=@(Get-CimInstance Win32_Processor | Select-Object Name,NumberOfCores,NumberOfLogicalProcessors);os=Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber;gpu=@(Get-CimInstance Win32_VideoController | Select-Object Name,DriverVersion);affinity=(Get-Process -Id $PID).ProcessorAffinity.ToInt64();power=(powercfg /getactivescheme)} | ConvertTo-Json -Depth 4 -Compress"], root);
	} else {
		platform.details = command("uname", ["-a"], root);
		platform.topology = command(process.platform === "darwin" ? "sysctl" : "lscpu", process.platform === "darwin" ? ["-n", "machdep.cpu.brand_string", "hw.physicalcpu"] : [], root);
		platform.devices = process.platform === "darwin" ? command("system_profiler", ["SPHardwareDataType", "SPDisplaysDataType", "-json"], root) : command("lspci", ["-nnk"], root);
	}

	platform.nvidia = command("nvidia-smi", ["--query-gpu=name,driver_version,pci.bus_id,memory.total,power.limit,clocks.max.sm,clocks.max.memory", "--format=csv,noheader"], root);

	const environment = Object.fromEntries(Object.entries(process.env).filter(([key]) => /^(GO|PACE|OMP|CUDA|VK_|VULKAN|OPENCL|OCL|RUST|RAYON|ZIG)/i.test(key))),
		runner = Object.fromEntries(files(join(root, "bench")).filter(path => path.endsWith(".mjs")).map(path => [basename(path), hash(path)]));

	return {
		schema: 1,
		started_utc: new Date().toISOString(),
		options: options,
		threads: threads,
		tools: tools,
		hardware: platform,
		environment: environment,
		runner: runner,
		runtime: { bun: process.versions.bun, node: process.versions.node, builder: command("builder", ["--version"], root), pace: command("pace", ["version"], root), zig: command("zig", ["version"], root) },
		git_status: command("git", ["status", "--short"], root),
		termination: windows ? "TerminateProcess after endpoint (no drain); saved files inspected after exit" : "SIGINT after endpoint, SIGKILL after 5s grace; saved files inspected after exit"
	};
}
