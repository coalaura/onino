#include "../bridge.c"
#include <assert.h>

static uint32_t memory_bits = 15;
static uint32_t allocated_type;
static uint32_t live_buffers;
static char mapped_buffer[ONINO_STREAM_BYTES];

static VKAPI_ATTR VkResult VKAPI_CALL create_buffer(VkDevice device, const VkBufferCreateInfo *info, const VkAllocationCallbacks *allocator, VkBuffer *buffer) {
	(void)device;
	(void)allocator;
	assert(info->flags == 0);
	assert(info->usage == (VK_BUFFER_USAGE_STORAGE_BUFFER_BIT | VK_BUFFER_USAGE_TRANSFER_DST_BIT | VK_BUFFER_USAGE_TRANSFER_SRC_BIT));
	assert(info->sharingMode == VK_SHARING_MODE_EXCLUSIVE);
	*buffer = (VkBuffer)(uintptr_t)1;
	live_buffers++;
	return VK_SUCCESS;
}

static VKAPI_ATTR void VKAPI_CALL buffer_requirements(VkDevice device, VkBuffer buffer, VkMemoryRequirements *requirements) {
	(void)device;
	(void)buffer;
	*requirements = (VkMemoryRequirements){ .size = ONINO_STREAM_BYTES, .alignment = 1, .memoryTypeBits = memory_bits };
}

static VKAPI_ATTR void VKAPI_CALL destroy_buffer(VkDevice device, VkBuffer buffer, const VkAllocationCallbacks *allocator) {
	(void)device;
	(void)buffer;
	(void)allocator;
	assert(live_buffers > 0);
	live_buffers--;
}

static VKAPI_ATTR VkResult VKAPI_CALL allocate_memory(VkDevice device, const VkMemoryAllocateInfo *info, const VkAllocationCallbacks *allocator, VkDeviceMemory *memory) {
	(void)device;
	(void)allocator;
	allocated_type = info->memoryTypeIndex;
	*memory = (VkDeviceMemory)(uintptr_t)1;
	return VK_SUCCESS;
}

static VKAPI_ATTR VkResult VKAPI_CALL bind_memory(VkDevice device, VkBuffer buffer, VkDeviceMemory memory, VkDeviceSize offset) {
	(void)device;
	(void)buffer;
	(void)memory;
	(void)offset;
	return VK_SUCCESS;
}

static VKAPI_ATTR VkResult VKAPI_CALL map_memory(VkDevice device, VkDeviceMemory memory, VkDeviceSize offset, VkDeviceSize size, VkMemoryMapFlags flags, void **mapped) {
	(void)device;
	(void)memory;
	(void)offset;
	(void)size;
	(void)flags;
	*mapped = mapped_buffer;
	return VK_SUCCESS;
}

static void check_allocation(onino_gpu *gpu, VkMemoryPropertyFlags properties, uint32_t expected_type) {
	onino_buffer buffer = {0};
	char error[256] = {0};
	int created = buffer_create(gpu, &buffer, ONINO_STREAM_BYTES, properties, error, sizeof(error));
	assert(created);
	assert(buffer.type == expected_type);
	assert(allocated_type == expected_type);
	assert(buffer.properties == gpu->memory.memoryTypes[expected_type].propertyFlags);
	destroy_buffer(gpu->device, buffer.buffer, NULL);
}

static void check_limit(onino_gpu *gpu, const VkPhysicalDeviceLimits *limits, uint32_t expected) {
	char error[256] = {0};
	uint32_t actual = automatic_streams(gpu, limits, error, sizeof(error));
	if (actual != expected) { fprintf(stderr, "automatic streams: got %u, want %u (%s)\n", actual, expected, error); abort(); }
	assert(live_buffers == 0);
	assert((error[0] != 0) == (expected == 0));
}

int main(void) {
	vkCreateBuffer = create_buffer;
	vkGetBufferMemoryRequirements = buffer_requirements;
	vkDestroyBuffer = destroy_buffer;
	vkAllocateMemory = allocate_memory;
	vkBindBufferMemory = bind_memory;
	vkMapMemory = map_memory;

	onino_gpu gpu = {0};
	VkMemoryPropertyFlags host = VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT;
	gpu.memory.memoryHeapCount = 3;
	gpu.memory.memoryHeaps[0].size = UINT64_C(24) << 30;
	gpu.memory.memoryHeaps[1].size = UINT64_C(64) << 30;
	gpu.memory.memoryHeaps[2].size = UINT64_C(246) << 20;
	gpu.memory.memoryTypeCount = 4;
	gpu.memory.memoryTypes[0] = (VkMemoryType){ .propertyFlags = VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT, .heapIndex = 1 };
	gpu.memory.memoryTypes[1] = (VkMemoryType){ .propertyFlags = VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, .heapIndex = 0 };
	gpu.memory.memoryTypes[2] = (VkMemoryType){ .propertyFlags = host, .heapIndex = 1 };
	gpu.memory.memoryTypes[3] = (VkMemoryType){ .propertyFlags = VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT | host, .heapIndex = 2 };
	VkPhysicalDeviceLimits limits = { .maxComputeWorkGroupCount = {65535, 65535, 65535}, .maxStorageBufferRange = UINT32_MAX };

	// The unused 246 MiB BAR heap must not reduce the 16384-stream ceiling.
	check_limit(&gpu, &limits, 16384);
	check_allocation(&gpu, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, 1);
	check_allocation(&gpu, host, 2);

	// memoryTypeBits can force either GPU or staging allocations onto that small heap.
	memory_bits = 13;
	check_limit(&gpu, &limits, 328);
	check_allocation(&gpu, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, 3);
	memory_bits = 11;
	check_limit(&gpu, &limits, 328);
	check_allocation(&gpu, host, 3);

	// Without a compatible coherent type, staging falls back to visible system RAM.
	memory_bits = 3;
	check_limit(&gpu, &limits, 16384);
	check_allocation(&gpu, host, 0);
	gpu.memory.memoryTypes[0].heapIndex = 2;
	check_limit(&gpu, &limits, 328);
	check_allocation(&gpu, host, 0);

	// A heap shared by GPU and staging uses the combined budget, not twice that budget.
	memory_bits = 8;
	check_limit(&gpu, &limits, 328);
	check_allocation(&gpu, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, 3);
	check_allocation(&gpu, host, 3);

	memory_bits = 15;
	limits.maxComputeWorkGroupCount[0] = 256;
	check_limit(&gpu, &limits, 256);
	limits.maxStorageBufferRange = 128 * ONINO_STREAM_BYTES;
	check_limit(&gpu, &limits, 128);

	// Missing required properties and an exhausted selected heap remain failures.
	memory_bits = 1;
	check_limit(&gpu, &limits, 0);
	memory_bits = 2;
	check_limit(&gpu, &limits, 0);
	memory_bits = 8;
	gpu.memory.memoryHeaps[2].size = 64 * ONINO_STREAM_BUDGET - 1;
	check_limit(&gpu, &limits, 0);
	puts("automatic stream sizing tests passed");
	return 0;
}
