# Building the optional Vulkan backend

Normal builds require neither CGO nor Vulkan. For the GPU backend, use Go 1.27.1 or compatible PACE, a C11 compiler and `-tags gpu`. The committed bridge includes volk and the required Vulkan headers and Go embeds the committed SPIR-V; building an executable does not require the Vulkan SDK or a shader compiler. The resulting executable loads the system Vulkan loader and driver at runtime. Vulkan 1.3, shader 64-bit integers, compute timestamps, host-visible staging memory, device-local storage memory and the checked workgroup/storage limits are required. Host-coherent staging is preferred; non-coherent allocations are explicitly flushed and invalidated.

On Windows, build with `builder build go --cgo --pace --dyn --no-gen -tags gpu`. Omit `--pace` for stock Go.

Linux requires dynamic linking against a libc compatible with the installed Vulkan loader and driver. Fully static musl GPU binaries are not supported. On a glibc Linux host with GCC, use `CGO_ENABLED=1 CC=gcc pace build -tags gpu -ldflags=-linkmode=external .`. For the verified Windows-to-Linux amd64 cross-build, run this in a POSIX shell with Zig and PACE on PATH:

```sh
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GOAMD64=v1 CC="zig cc -target x86_64-linux-gnu.2.31" pace build -trimpath -buildvcs=false -ldflags=-linkmode=external -tags gpu -o onino-linux .
```

Replace `pace` with `go` for stock Go. The explicit GNU target is required with builder versions that select a musl target even when `--dyn` is supplied. Verify that the Linux artifact has an ELF interpreter such as `/lib64/ld-linux-x86-64.so.2` and a dynamic libc dependency; a successful link alone does not establish that the driver can be loaded. The installed driver may impose a newer libc requirement than the application's selected baseline.

GPU builds add `--gpu off|auto|<index>`, defaulting to `off`. With GPU search enabled, `--cpu 0` and `--cpu off` both disable CPU search workers; the host still performs GPU orchestration, match verification and storage. For example, `onino --gpu auto --cpu off somethingrare.` searches only on the GPU; `onino --gpu auto --cpu 1 somethingrare.` also runs one pinned CPU worker when topology information is available. `auto` prefers a capable discrete device; an explicit index follows Vulkan physical-device enumeration. Initialization errors are reported rather than silently falling back to CPU search.

GPU search accepts one through eight lowercase literal base32 prefixes, each ending in a dot and containing one through 51 characters before it. Other pattern forms are rejected when GPU search is enabled. The GPU filters the first twelve characters and the host verifies the complete pattern, so longer supported prefixes remain exact. The current runtime-validated search target is the RTX 5090 on Windows; Linux cross-compilation and vendor-neutral shaders do not establish runtime support for every driver.

Omitted `--gpu-streams` and `--gpu-rounds` values are calibrated with the requested CPU workers active, using completed real-search work. An explicit flag fixes that parameter; specifying both skips performance exploration while retaining bounded-work validation. Initial selection targets a ten-second total startup budget, including driver initialization. Driver calls are not interruptible. `--gpu-diagnostics` reports allocation properties, trials, same-window counters, submission costs and lifecycle timing. The ordinary progress report remains shared and four-secondly. Saved matches and calibration counts are retained across configuration changes.

## Shader generation

The checked-in shaders were generated with glslc and SPIRV-Tools from Vulkan SDK 1.4.328.1. Run these commands from the repository root, with those tools on PATH:

```sh
glslc --target-env=vulkan1.3 -O internal/gpu/shaders/search.comp -o internal/gpu/shaders/search.spv
spirv-val --target-env vulkan1.3 internal/gpu/shaders/search.spv
glslc --target-env=vulkan1.3 -O internal/gpu/shaders/arithmetic.comp -o internal/gpu/shaders/arithmetic.spv
spirv-val --target-env vulkan1.3 internal/gpu/shaders/arithmetic.spv
glslc --target-env=vulkan1.3 -O -DONINO_DIAGNOSTIC=1 internal/gpu/shaders/search.comp -o internal/gpu/shaders/diagnostic.spv
spirv-val --target-env vulkan1.3 internal/gpu/shaders/diagnostic.spv
glslc --target-env=vulkan1.3 -O -DONINO_INITIAL_STEPS=4294967170u internal/gpu/shaders/search.comp -o internal/gpu/shaders/epoch.spv
spirv-val --target-env vulkan1.3 internal/gpu/shaders/epoch.spv
```

The arithmetic, full-encoding and epoch-boundary diagnostic shaders are embedded only in correctness tests. The epoch variant changes the initial step counter to exercise exhaustion without traversing billions of candidates. Source edits require regenerating and validating the affected SPIR-V before testing. There is deliberately no automatic SDK download or shader compilation during normal builds.

## Device correctness checks

Set `ONINO_VULKAN_TEST=1` and make `VK_LAYER_KHRONOS_validation` available to the Vulkan loader (using `VK_LAYER_PATH` if needed), then run `builder test go --cgo --pace --dyn --no-gen -tags gpu -vet=off ./...`. Device tests require validation and fail on validation errors. Set `VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT` to include synchronization validation. Tests select physical device zero by default; `ONINO_VULKAN_DEVICE` overrides the index. Without the environment opt-in, device tests skip, allowing compilation and host-only tests on systems without Vulkan. Repeat without `--pace` for stock Go. Use the custom `vet` tool for static checks. Timing runs must leave validation disabled.
