# Vulkan bridge dependencies

The bridge statically compiles volk and uses Vulkan-Headers without linking an SDK library. The executable loads the installed Vulkan loader at runtime.

- volk: `zeux/volk`, revision `f30088b3f4160810b53e19258dd2f7395e5f0ba3` (`vulkan-sdk-1.4.328.1`), MIT license in `volk.h` and `volk.c`.
- Vulkan-Headers: `KhronosGroup/Vulkan-Headers`, revision `19725e4d48082fe78e26622b15d3080ccd54112b` (`vulkan-sdk-1.4.328.1`). Only `vk_platform.h`, `vulkan_core.h`, their required `vk_video` includes, and the upstream license are included. Copyright and license identifiers are preserved in each header.

These files are unmodified upstream sources. Updating them requires updating both revisions together and repeating shader validation and the Vulkan correctness tests.
