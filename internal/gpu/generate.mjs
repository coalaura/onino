import { copyFileSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve, join, dirname } from "node:path";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";

const root = resolve(import.meta.dir, "../.."), source = join(root, "internal/gpu/shaders");
const [compilerArgument, validatorArgument, manifestArgument, route = "glslang"] = process.argv.slice(2);
if (!compilerArgument || !validatorArgument || !manifestArgument || !["glslang", "glslc"].includes(route)) {
	throw new Error("Usage: bun internal/gpu/generate.mjs COMPILER VALIDATOR MANIFEST [glslang|glslc]");
}

const compiler = resolve(compilerArgument), validator = resolve(validatorArgument), manifestPath = resolve(manifestArgument);
const temporary = mkdtempSync(join(dirname(manifestPath), ".generate-"));
const hash = path => createHash("sha256").update(readFileSync(path)).digest("hex");
const manifest = { compiler: compiler, validator: validator, route: route, source: {}, commands: [], outputs: {}, temporary: temporary };

function execute(executable, args) {
	const result = spawnSync(executable, args, { cwd: root, encoding: "utf8", timeout: 120000 });
	manifest.commands.push({ executable: executable, args: args, exit: result.status, stdout: result.stdout, stderr: result.stderr, error: result.error?.message });
	writeFileSync(manifestPath, JSON.stringify(manifest, null, "\t"));
	if (result.error || result.status !== 0) {
		throw new Error(`Generation stopped; embedded artifacts untouched. See ${manifestPath}`);
	}
	return result.stdout;
}

manifest.version = execute(compiler, ["--version"]);
manifest.validatorVersion = execute(validator, ["--version"]);
for (const name of readdirSync(source).filter(name => /\.(comp|glsl)$/.test(name))) {
	manifest.source[name] = hash(join(source, name));
}

const flags = route === "glslang" ? ["-V", "--target-env", "vulkan1.3", "-Os"] : ["--target-env=vulkan1.3", "-O"];
const modules = [
	{ name: "search", source: "search", defines: [] },
	{ name: "arithmetic", source: "arithmetic", defines: [] },
	{ name: "diagnostic", source: "search", defines: ["-DONINO_DIAGNOSTIC=1"] },
	{ name: "epoch", source: "search", defines: ["-DONINO_INITIAL_STEPS=4294967167u"] }
];

for (const module of modules) {
	const output = join(temporary, `${module.name}.spv`);
	execute(compiler, [...flags, ...module.defines, join(source, `${module.source}.comp`), "-o", output]);
	execute(validator, ["--target-env", "vulkan1.3", output]);
	manifest.outputs[module.name] = hash(output);
}

// No embedded artifact is replaced until every fresh module passes validation.
for (const module of modules) {
	copyFileSync(join(temporary, `${module.name}.spv`), join(source, `${module.name}.spv`));
}
manifest.completed = true;
writeFileSync(manifestPath, JSON.stringify(manifest, null, "\t"));
console.log(`Validated and installed all four modules; provenance: ${manifestPath}`);
