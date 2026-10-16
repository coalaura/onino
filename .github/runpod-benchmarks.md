### runpod - RTX PRO 6000

Location: US-PA-1
OS: ubuntu 22.04
GPU: NVIDIA RTX PRO 6000 Blackwell Server Edition
vCPU: 36 (Intel(R) Xeon(R) 6960P)
Image: runpod/base:1.0.3-ubuntu2204

```
Thu Oct 15 22:21:38 2026
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 615.71.09              KMD Version: 615.71.09     CUDA UMD Version: 13.4     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA RTX PRO 6000 Blac...    On  |   00000000:01:00.0 Off |                    0 |
| N/A   23C    P8             38W /  600W |       0MiB /  97887MiB |      0%      Default |
|                                         |                        |             Disabled |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
|  No running processes found                                                             |
+-----------------------------------------------------------------------------------------+

==========
VULKANINFO
==========

Vulkan Instance Version: 1.3.204


Instance Extensions: count = 20
-------------------------------
VK_EXT_acquire_drm_display             : extension revision 1
VK_EXT_acquire_xlib_display            : extension revision 1
VK_EXT_debug_report                    : extension revision 10
VK_EXT_debug_utils                     : extension revision 2
VK_EXT_direct_mode_display             : extension revision 1
VK_EXT_display_surface_counter         : extension revision 1
VK_EXT_swapchain_colorspace            : extension revision 5
VK_KHR_device_group_creation           : extension revision 1
VK_KHR_display                         : extension revision 23
VK_KHR_external_fence_capabilities     : extension revision 1
VK_KHR_external_memory_capabilities    : extension revision 1
VK_KHR_external_semaphore_capabilities : extension revision 1
VK_KHR_get_display_properties2         : extension revision 1
VK_KHR_get_physical_device_properties2 : extension revision 2
VK_KHR_get_surface_capabilities2       : extension revision 1
VK_KHR_surface                         : extension revision 25
VK_KHR_surface_protected_capabilities  : extension revision 1
VK_KHR_wayland_surface                 : extension revision 6
VK_KHR_xcb_surface                     : extension revision 6
VK_KHR_xlib_surface                    : extension revision 6

Instance Layers: count = 2
--------------------------
VK_LAYER_NV_optimus NVIDIA Optimus layer      1.4.351  version 1
VK_LAYER_NV_present NVIDIA Presentation Layer 1.4.351  version 1

Devices:
========
GPU0:
        apiVersion         = 4211039 (1.4.351)
        driverVersion      = 2580660800 (0x99d1c240)
        vendorID           = 0x10de
        deviceID           = 0x2bb5
        deviceType         = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
        deviceName         = NVIDIA RTX PRO 6000 Blackwell Server Edition
        driverID           = DRIVER_ID_NVIDIA_PROPRIETARY
        driverName         = NVIDIA
        driverInfo         = 615.71.09
        conformanceVersion = 1.4.3.3
        deviceUUID         = 13988867-0280-b3c3-1876-2315156f3276
        driverUUID         = e960882b-0170-5057-bf3e-c686505f8776
```

```
root@8ded675ddfe5:/# onino --cpu off --gpu auto somethingrare.
Initializing GPU...
Tuning GPU; calibration performs useful search.
5s | tuning | 33.2G checked | 6.63G/s | saved 0 | wait 50% ~122y / 95% ~528y
Search setup
  Patterns    1 from command line (somethingrare.)
  CPU         disabled
  Features    AVX2, BMI2, ADX, AVX-512 IFMA
  Build       amd64: portable, bmi2, bmi2-adx, ifma
  Engine      inactive (--simd=auto)
  GPU         NVIDIA RTX PRO 6000 Blackwell Server Edition
              16384 streams (auto), 13 rounds (auto)
  Startup     input + host setup 0.02s
              GPU initialization 1.65s
              first GPU submission at 1.68s since start
              configuration selected at 8.37s since start
  Output      matches
  Stop        Ctrl+C (drains accepted matches)
  Candidates  50% ~25.6E; 95% ~111E
  Waits estimate a new match from now at the recent combined rate.
  Uniform-key model; overlaps accounted for, large unions approximated.
10s | 92.5G checked | 11.9G/s | saved 0 | wait 50% ~68.3y / 95% ~295y
15s | 156G checked | 12.6G/s | saved 0 | wait 50% ~64.1y / 95% ~277y
20s | 219G checked | 12.6G/s | saved 0 | wait 50% ~64.1y / 95% ~277y
25s | 283G checked | 12.8G/s | saved 0 | wait 50% ~63.4y / 95% ~274y
30s | 347G checked | 12.8G/s | saved 0 | wait 50% ~63.2y / 95% ~273y
35s | 411G checked | 12.9G/s | saved 0 | wait 50% ~63y / 95% ~272y
40s | 476G checked | 12.8G/s | saved 0 | wait 50% ~63.1y / 95% ~273y
45s | 540G checked | 12.8G/s | saved 0 | wait 50% ~63.1y / 95% ~273y
50s | 604G checked | 12.8G/s | saved 0 | wait 50% ~63.1y / 95% ~273y
55s | 668G checked | 12.8G/s | saved 0 | wait 50% ~63.1y / 95% ~273y
1m0s | 733G checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~272y
1m5s | 797G checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m10s | 862G checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m15s | 926G checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m20s | 991G checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m25s | 1.06T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m30s | 1.12T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m35s | 1.18T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m40s | 1.25T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m45s | 1.31T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m50s | 1.38T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
1m55s | 1.44T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
2m0s | 1.51T checked | 12.9G/s | saved 0 | wait 50% ~62.8y / 95% ~271y
^CStopped: interrupted | elapsed 2m0.888s
  Overall averages use the same full-run wall clock.
  CPU    0 checked | 0 keys/s avg | disabled
  GPU    1,517,387,382,784 checked | 12.6G keys/s avg | enabled (NVIDIA RTX PRO 6000 Blackwell Server Edition)
  Total  1,517,387,382,784 checked | 12.6G keys/s avg
  Saved  0 matches
  Output matches

root@8ded675ddfe5:/# ./prefix32-v2.2.0-linux-x86_64-fast --gpu --gpu-effort 10 somethingrare
prefix32 v2.2.0
  Prefix:   somethingrare (65 bits, ~1 in 36.9E keys)
  Warning:  ~1.17K years at 1 billion keys/sec
  Device:   NVIDIA RTX PRO 6000 Blackwell Server Edition (188 CUs, OpenCL)
  Stopping: continuous (Ctrl+C to quit)

  Auto-tuning... (press Enter to stop early)
    BATCH=16: GS=1540096 NB=256 -> 4.29B/s
    BATCH=32: GS=48128 NB=256 -> 4.41B/s
    BATCH=64: GS=1540096 NB=256 -> 7.31B/s
    BATCH=128: GS=1540096 NB=128 -> 8.76B/s
    BATCH=256: GS=1540096 NB=64 -> 9.55B/s
    BATCH=512: GS=48128 NB=128 -> 10.1B/s
    BATCH=1024: GS=48128 NB=128 -> 10.3B/s
  Optimal:  BATCH=1024 GS=48128 NB=128 -> 10.3B/s
  Dispatch: 6.31B candidates

  2m00s | 1.24T keys | 10.3B/s | ETA 41436d20h      ^C
```

### runpod - RTX 4090

Location: EU-CZ-1
OS: ubuntu 22.04
GPU: NVIDIA GeForce RTX 4090
vCPU: 36 (AMD EPYC 7763 64-Core Processor)
Image: runpod/base:1.0.3-ubuntu2204

```
Thu Oct 15 22:32:15 2026
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 610.57.04              KMD Version: 610.57.04     CUDA UMD Version: 13.3     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 4090        On  |   00000000:81:00.0 Off |                  Off |
| 40%   31C    P8             29W /  450W |       1MiB /  24564MiB |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
|  No running processes found                                                             |
+-----------------------------------------------------------------------------------------+

==========
VULKANINFO
==========

Vulkan Instance Version: 1.3.204


Instance Extensions: count = 20
-------------------------------
VK_EXT_acquire_drm_display             : extension revision 1
VK_EXT_acquire_xlib_display            : extension revision 1
VK_EXT_debug_report                    : extension revision 10
VK_EXT_debug_utils                     : extension revision 2
VK_EXT_direct_mode_display             : extension revision 1
VK_EXT_display_surface_counter         : extension revision 1
VK_EXT_swapchain_colorspace            : extension revision 5
VK_KHR_device_group_creation           : extension revision 1
VK_KHR_display                         : extension revision 23
VK_KHR_external_fence_capabilities     : extension revision 1
VK_KHR_external_memory_capabilities    : extension revision 1
VK_KHR_external_semaphore_capabilities : extension revision 1
VK_KHR_get_display_properties2         : extension revision 1
VK_KHR_get_physical_device_properties2 : extension revision 2
VK_KHR_get_surface_capabilities2       : extension revision 1
VK_KHR_surface                         : extension revision 25
VK_KHR_surface_protected_capabilities  : extension revision 1
VK_KHR_wayland_surface                 : extension revision 6
VK_KHR_xcb_surface                     : extension revision 6
VK_KHR_xlib_surface                    : extension revision 6

Instance Layers: count = 2
--------------------------
VK_LAYER_NV_optimus NVIDIA Optimus layer      1.4.341  version 1
VK_LAYER_NV_present NVIDIA Presentation Layer 1.4.341  version 1

Devices:
========
GPU0:
        apiVersion         = 4211029 (1.4.341)
        driverVersion      = 2559459584 (0x988e4100)
        vendorID           = 0x10de
        deviceID           = 0x2684
        deviceType         = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
        deviceName         = NVIDIA GeForce RTX 4090
        driverID           = DRIVER_ID_NVIDIA_PROPRIETARY
        driverName         = NVIDIA
        driverInfo         = 610.57.04
        conformanceVersion = 1.4.3.3
        deviceUUID         = 264be802-ad92-5d0f-f1fb-9ede66a06f6e
        driverUUID         = 9992f558-9329-580d-a9f3-a38ee24bc64d
```

```
root@94a67516b735:/# onino --cpu off --gpu auto somethingrare.
Initializing GPU...
Tuning GPU; calibration performs useful search.
5s | tuning | 21.1G checked | 4.21G/s | saved 0 | wait 50% ~192y / 95% ~831y
Search setup
  Patterns    1 from command line (somethingrare.)
  CPU         disabled
  Features    AVX2, BMI2, ADX
  Build       amd64: portable, bmi2, bmi2-adx, ifma
  Engine      inactive (--simd=auto)
  GPU         NVIDIA GeForce RTX 4090
              16384 streams (auto), 4 rounds (auto)
  Startup     input + host setup 0.10s
              GPU initialization 2.37s
              first GPU submission at 2.49s since start
              configuration selected at 8.56s since start
  Output      matches
  Stop        Ctrl+C (drains accepted matches)
  Candidates  50% ~25.6E; 95% ~111E
  Waits estimate a new match from now at the recent combined rate.
  Uniform-key model; overlaps accounted for, large unions approximated.
10s | 74G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
15s | 128G checked | 10.8G/s | saved 0 | wait 50% ~75.2y / 95% ~325y
20s | 182G checked | 10.8G/s | saved 0 | wait 50% ~75.3y / 95% ~325y
25s | 235G checked | 10.7G/s | saved 0 | wait 50% ~75.6y / 95% ~327y
30s | 289G checked | 10.7G/s | saved 0 | wait 50% ~75.9y / 95% ~328y
35s | 342G checked | 10.6G/s | saved 0 | wait 50% ~76.1y / 95% ~329y
40s | 395G checked | 10.6G/s | saved 0 | wait 50% ~76.2y / 95% ~329y
45s | 448G checked | 10.6G/s | saved 0 | wait 50% ~76.3y / 95% ~330y
50s | 501G checked | 10.6G/s | saved 0 | wait 50% ~76.4y / 95% ~330y
55s | 554G checked | 10.6G/s | saved 0 | wait 50% ~76.4y / 95% ~330y
1m0s | 607G checked | 10.6G/s | saved 0 | wait 50% ~76.5y / 95% ~331y
1m5s | 660G checked | 10.6G/s | saved 0 | wait 50% ~76.5y / 95% ~331y
1m10s | 713G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m15s | 766G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m20s | 819G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m25s | 872G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m30s | 925G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m35s | 978G checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m40s | 1.03T checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m45s | 1.08T checked | 10.6G/s | saved 0 | wait 50% ~76.6y / 95% ~331y
1m50s | 1.14T checked | 10.6G/s | saved 0 | wait 50% ~76.7y / 95% ~331y
1m55s | 1.19T checked | 10.6G/s | saved 0 | wait 50% ~76.7y / 95% ~332y
2m0s | 1.24T checked | 10.6G/s | saved 0 | wait 50% ~76.7y / 95% ~331y
^CStopped: interrupted | elapsed 2m1.245s
  Overall averages use the same full-run wall clock.
  CPU    0 checked | 0 keys/s avg | disabled
  GPU    1,253,865,881,600 checked | 10.3G keys/s avg | enabled (NVIDIA GeForce RTX 4090)
  Total  1,253,865,881,600 checked | 10.3G keys/s avg
  Saved  0 matches
  Output matches

root@94a67516b735:/# ./prefix32-v2.2.0-linux-x86_64-fast --gpu --gpu-effort 10 somethingrare
prefix32 v2.2.0
  Prefix:   somethingrare (65 bits, ~1 in 36.9E keys)
  Warning:  ~1.17K years at 1 billion keys/sec
  Device:   NVIDIA GeForce RTX 4090 (128 CUs, OpenCL)
  Stopping: continuous (Ctrl+C to quit)

  Auto-tuning... (press Enter to stop early)
    BATCH=16: GS=1048576 NB=256 -> 3.69B/s
    BATCH=32: GS=262144 NB=256 -> 5.02B/s
    BATCH=64: GS=262144 NB=256 -> 5.42B/s
    BATCH=128: GS=1048576 NB=128 -> 6.34B/s
    BATCH=256: GS=1048576 NB=64 -> 7.04B/s
    BATCH=512: GS=262144 NB=64 -> 6.95B/s
    BATCH=1024: skip (Build(BATCH=1024): "CL_BUILD_PROGRAM_FAILURE, build log: (): Warning: Function vanity_search is a kernel, so overriding noinline attribute. The function may be inlined when called.\nptxas error   : Entry function 'vanity_search' uses too much shared data (0xc008 bytes, 0xc000 max)\n\n")
  Optimal:  BATCH=256 GS=1048576 NB=64 -> 7.04B/s
  Dispatch: 17.2B candidates

  2m02s | 845B keys | 6.92B/s | ETA 61706d10h      ^C
```

### runpod - RTX 3090

Location: EU-CZ-1
OS: ubuntu 22.04
GPU: NVIDIA GeForce RTX 3090
vCPU: 32 (AMD EPYC 7763 64-Core Processor)
Image: runpod/base:1.0.3-ubuntu2204

```
Thu Oct 15 23:27:04 2026
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 610.57.04              KMD Version: 610.57.04     CUDA UMD Version: 13.3     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 3090        On  |   00000000:41:00.0 Off |                  N/A |
| 30%   28C    P8             22W /  350W |       1MiB /  24576MiB |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
|  No running processes found                                                             |
+-----------------------------------------------------------------------------------------+

==========
VULKANINFO
==========

Vulkan Instance Version: 1.3.204


Instance Extensions: count = 20
-------------------------------
VK_EXT_acquire_drm_display             : extension revision 1
VK_EXT_acquire_xlib_display            : extension revision 1
VK_EXT_debug_report                    : extension revision 10
VK_EXT_debug_utils                     : extension revision 2
VK_EXT_direct_mode_display             : extension revision 1
VK_EXT_display_surface_counter         : extension revision 1
VK_EXT_swapchain_colorspace            : extension revision 5
VK_KHR_device_group_creation           : extension revision 1
VK_KHR_display                         : extension revision 23
VK_KHR_external_fence_capabilities     : extension revision 1
VK_KHR_external_memory_capabilities    : extension revision 1
VK_KHR_external_semaphore_capabilities : extension revision 1
VK_KHR_get_display_properties2         : extension revision 1
VK_KHR_get_physical_device_properties2 : extension revision 2
VK_KHR_get_surface_capabilities2       : extension revision 1
VK_KHR_surface                         : extension revision 25
VK_KHR_surface_protected_capabilities  : extension revision 1
VK_KHR_wayland_surface                 : extension revision 6
VK_KHR_xcb_surface                     : extension revision 6
VK_KHR_xlib_surface                    : extension revision 6

Instance Layers: count = 2
--------------------------
VK_LAYER_NV_optimus NVIDIA Optimus layer      1.4.341  version 1
VK_LAYER_NV_present NVIDIA Presentation Layer 1.4.341  version 1

Devices:
========
GPU0:
        apiVersion         = 4211029 (1.4.341)
        driverVersion      = 2559459584 (0x988e4100)
        vendorID           = 0x10de
        deviceID           = 0x2204
        deviceType         = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
        deviceName         = NVIDIA GeForce RTX 3090
        driverID           = DRIVER_ID_NVIDIA_PROPRIETARY
        driverName         = NVIDIA
        driverInfo         = 610.57.04
        conformanceVersion = 1.4.3.3
        deviceUUID         = c94ba231-1293-62de-a86d-5f9ecd27b8a0
        driverUUID         = 9992f558-9329-580d-a9f3-a38ee24bc64d
```

```
root@940ceeef823b:/# onino --cpu off --gpu auto somethingrare.
Initializing GPU...
Tuning GPU; calibration performs useful search.
5s | tuning | 8.61G checked | 1.72G/s | saved 0 | wait 50% ~471y / 95% ~2.04ky
Search setup
  Patterns    1 from command line (somethingrare.)
  CPU         disabled
  Features    AVX2, BMI2, ADX
  Build       amd64: portable, bmi2, bmi2-adx, ifma
  Engine      inactive (--simd=auto)
  GPU         NVIDIA GeForce RTX 3090
              16384 streams (auto), 5 rounds (auto)
  Startup     input + host setup 0.08s
              GPU initialization 2.38s
              first GPU submission at 2.47s since start
              configuration selected at 8.78s since start
  Output      matches
  Stop        Ctrl+C (drains accepted matches)
  Candidates  50% ~25.6E; 95% ~111E
  Waits estimate a new match from now at the recent combined rate.
  Uniform-key model; overlaps accounted for, large unions approximated.
10s | 30.1G checked | 4.29G/s | saved 0 | wait 50% ~189y / 95% ~816y
15s | 51.7G checked | 4.32G/s | saved 0 | wait 50% ~188y / 95% ~811y
20s | 73.3G checked | 4.33G/s | saved 0 | wait 50% ~187y / 95% ~808y
25s | 94.9G checked | 4.32G/s | saved 0 | wait 50% ~188y / 95% ~811y
30s | 116G checked | 4.3G/s | saved 0 | wait 50% ~188y / 95% ~814y
35s | 138G checked | 4.29G/s | saved 0 | wait 50% ~189y / 95% ~816y
40s | 159G checked | 4.28G/s | saved 0 | wait 50% ~189y / 95% ~818y
45s | 181G checked | 4.27G/s | saved 0 | wait 50% ~190y / 95% ~820y
50s | 202G checked | 4.27G/s | saved 0 | wait 50% ~190y / 95% ~821y
55s | 223G checked | 4.26G/s | saved 0 | wait 50% ~190y / 95% ~822y
1m0s | 245G checked | 4.26G/s | saved 0 | wait 50% ~190y / 95% ~822y
1m5s | 266G checked | 4.26G/s | saved 0 | wait 50% ~190y / 95% ~822y
1m10s | 287G checked | 4.26G/s | saved 0 | wait 50% ~190y / 95% ~823y
1m15s | 308G checked | 4.25G/s | saved 0 | wait 50% ~191y / 95% ~824y
1m20s | 330G checked | 4.24G/s | saved 0 | wait 50% ~191y / 95% ~825y
1m25s | 351G checked | 4.24G/s | saved 0 | wait 50% ~191y / 95% ~825y
1m30s | 372G checked | 4.24G/s | saved 0 | wait 50% ~191y / 95% ~826y
1m35s | 393G checked | 4.24G/s | saved 0 | wait 50% ~191y / 95% ~826y
1m40s | 414G checked | 4.23G/s | saved 0 | wait 50% ~191y / 95% ~827y
1m45s | 436G checked | 4.24G/s | saved 0 | wait 50% ~191y / 95% ~827y
1m50s | 457G checked | 4.23G/s | saved 0 | wait 50% ~191y / 95% ~828y
1m55s | 478G checked | 4.23G/s | saved 0 | wait 50% ~191y / 95% ~828y
2m0s | 499G checked | 4.23G/s | saved 0 | wait 50% ~192y / 95% ~828y
^CStopped: interrupted | elapsed 2m1.444s
  Overall averages use the same full-run wall clock.
  CPU    0 checked | 0 keys/s avg | disabled
  GPU    504,777,932,800 checked | 4.16G keys/s avg | enabled (NVIDIA GeForce RTX 3090)
  Total  504,777,932,800 checked | 4.16G keys/s avg
  Saved  0 matches
  Output matches

root@940ceeef823b:/# ./prefix32-v2.2.0-linux-x86_64-fast --gpu --gpu-effort 10 somethingrare
prefix32 v2.2.0
  Prefix:   somethingrare (65 bits, ~1 in 36.9E keys)
  Warning:  ~1.17K years at 1 billion keys/sec
  Device:   NVIDIA GeForce RTX 3090 (82 CUs, OpenCL)
  Stopping: continuous (Ctrl+C to quit)

  Auto-tuning... (press Enter to stop early)
    BATCH=16: GS=20992 NB=256 -> 1.34B/s
    BATCH=32: GS=20992 NB=256 -> 2.08B/s
    BATCH=64: GS=41984 NB=256 -> 2.91B/s
    BATCH=128: GS=671744 NB=64 -> 3.56B/s
    BATCH=256: GS=41984 NB=64 -> 4.05B/s
    BATCH=512: GS=20992 NB=64 -> 4.36B/s
    BATCH=1024: skip (Build(BATCH=1024): "CL_BUILD_PROGRAM_FAILURE, build log: (): Warning: Function vanity_search is a kernel, so overriding noinline attribute. The function may be inlined when called.\nptxas error   : Entry function 'vanity_search' uses too much shared data (0xc008 bytes, 0xc000 max)\n\n")
  Optimal:  BATCH=512 GS=20992 NB=64 -> 4.36B/s
  Dispatch: 689M candidates

  2m01s | 521B keys | 4.29B/s | ETA 99493d04h      ^C
```