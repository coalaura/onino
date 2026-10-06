//go:build gpu

#include "bridge.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>
#include "vendor/volk.c"

_Static_assert(sizeof(onino_command) == 132, "shader command layout");
_Static_assert(sizeof(onino_hit) == 24, "shader hit layout");
_Static_assert(sizeof(onino_result) == 16, "shader result header layout");

typedef struct {
	VkBuffer buffer;
	VkDeviceMemory memory;
	void *mapped;
} onino_buffer;
typedef struct {
	onino_buffer commands;
	onino_buffer results;
	VkDescriptorSet descriptors;
	VkCommandBuffer command;
	VkFence fence;
	int submitted;
} onino_slot;
struct onino_gpu {
	VkInstance instance;
	VkDebugUtilsMessengerEXT messenger;
	VkPhysicalDevice physical;
	VkDevice device;
	VkQueue queue;
	VkPhysicalDeviceMemoryProperties memory;
	VkPipeline pipeline;
	VkPipelineLayout layout;
	VkDescriptorSetLayout descriptors;
	VkDescriptorPool descriptor_pool;
	VkCommandPool command_pool;
	VkQueryPool queries;
	onino_buffer streams;
	onino_buffer table;
	onino_buffer table_upload;
	onino_slot slots[2];
	uint32_t stream_count;
	uint32_t capacity;
	uint32_t timestamp_bits;
	float timestamp_period;
	uint64_t last_timestamp;
	int have_timestamp;
	atomic_uint validation_errors;
	char name[VK_MAX_PHYSICAL_DEVICE_NAME_SIZE];
};

static int fail(char *error, size_t size, const char *operation, VkResult result) {
	snprintf(error, size, "%s (Vulkan result %d)", operation, result);
	return 0;
}
#define CHECK(operation) do { VkResult result = (operation); if (result != VK_SUCCESS) { fail(error, error_size, #operation, result); goto failure; } } while (0)

static VKAPI_ATTR VkBool32 VKAPI_CALL validation_message(VkDebugUtilsMessageSeverityFlagBitsEXT severity, VkDebugUtilsMessageTypeFlagsEXT type, const VkDebugUtilsMessengerCallbackDataEXT *data, void *user) {
	(void)type;
	onino_gpu *gpu = user;
	if (severity & VK_DEBUG_UTILS_MESSAGE_SEVERITY_ERROR_BIT_EXT) { atomic_fetch_add(&gpu->validation_errors, 1); }
	fprintf(stderr, "Vulkan validation: %s\n", data->pMessage);
	return VK_FALSE;
}
static int buffer_create(onino_gpu *gpu, onino_buffer *buffer, VkDeviceSize size, VkMemoryPropertyFlags properties, char *error, size_t error_size) {
	VkBufferCreateInfo info = { .sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO, .size = size, .usage = VK_BUFFER_USAGE_STORAGE_BUFFER_BIT | VK_BUFFER_USAGE_TRANSFER_DST_BIT | VK_BUFFER_USAGE_TRANSFER_SRC_BIT, .sharingMode = VK_SHARING_MODE_EXCLUSIVE };
	CHECK(vkCreateBuffer(gpu->device, &info, NULL, &buffer->buffer));
	VkMemoryRequirements requirements;
	vkGetBufferMemoryRequirements(gpu->device, buffer->buffer, &requirements);
	uint32_t memory_index;
	for (memory_index = 0; memory_index < gpu->memory.memoryTypeCount; memory_index++) {
		if ((requirements.memoryTypeBits & (1u << memory_index)) && (gpu->memory.memoryTypes[memory_index].propertyFlags & properties) == properties) { break; }
	}
	if (memory_index == gpu->memory.memoryTypeCount) { fail(error, error_size, "required Vulkan memory type unavailable", VK_ERROR_FEATURE_NOT_PRESENT); goto failure; }
	VkMemoryAllocateInfo allocate = { .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, .allocationSize = requirements.size, .memoryTypeIndex = memory_index };
	CHECK(vkAllocateMemory(gpu->device, &allocate, NULL, &buffer->memory));
	CHECK(vkBindBufferMemory(gpu->device, buffer->buffer, buffer->memory, 0));
	if (properties & VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT) {
		CHECK(vkMapMemory(gpu->device, buffer->memory, 0, VK_WHOLE_SIZE, 0, &buffer->mapped));
		memset(buffer->mapped, 0, (size_t)size);
	}
	return 1;
failure:
	return 0;
}
static void buffer_destroy(onino_gpu *gpu, onino_buffer *buffer) {
	if (buffer->mapped) { vkUnmapMemory(gpu->device, buffer->memory); }
	if (buffer->buffer) { vkDestroyBuffer(gpu->device, buffer->buffer, NULL); }
	if (buffer->memory) { vkFreeMemory(gpu->device, buffer->memory, NULL); }
	memset(buffer, 0, sizeof(*buffer));
}

onino_gpu *onino_open(int index, int validation, uint32_t streams, uint32_t capacity, const void *shader, size_t shader_size, const void *table, size_t table_size, char *error, size_t error_size) {
	onino_gpu *gpu = calloc(1, sizeof(*gpu));
	if (!gpu) { fail(error, error_size, "allocate Vulkan controller", VK_ERROR_OUT_OF_HOST_MEMORY); return NULL; }
	atomic_init(&gpu->validation_errors, 0);
	gpu->stream_count = streams;
	gpu->capacity = capacity;
	CHECK(volkInitialize());
	if (volkGetInstanceVersion() < VK_API_VERSION_1_3) { fail(error, error_size, "Vulkan 1.3 loader required", VK_ERROR_INCOMPATIBLE_DRIVER); goto failure; }
	const char *layer = "VK_LAYER_KHRONOS_validation";
	const char *extension = VK_EXT_DEBUG_UTILS_EXTENSION_NAME;
	VkApplicationInfo app = { .sType = VK_STRUCTURE_TYPE_APPLICATION_INFO, .pApplicationName = "onino", .apiVersion = VK_API_VERSION_1_3 };
	VkInstanceCreateInfo instance = { .sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO, .pApplicationInfo = &app, .enabledLayerCount = validation ? 1 : 0, .ppEnabledLayerNames = &layer, .enabledExtensionCount = validation ? 1 : 0, .ppEnabledExtensionNames = &extension };
	CHECK(vkCreateInstance(&instance, NULL, &gpu->instance));
	volkLoadInstance(gpu->instance);
	if (validation) {
		VkDebugUtilsMessengerCreateInfoEXT debug = { .sType = VK_STRUCTURE_TYPE_DEBUG_UTILS_MESSENGER_CREATE_INFO_EXT, .messageSeverity = VK_DEBUG_UTILS_MESSAGE_SEVERITY_WARNING_BIT_EXT | VK_DEBUG_UTILS_MESSAGE_SEVERITY_ERROR_BIT_EXT, .messageType = VK_DEBUG_UTILS_MESSAGE_TYPE_GENERAL_BIT_EXT | VK_DEBUG_UTILS_MESSAGE_TYPE_VALIDATION_BIT_EXT | VK_DEBUG_UTILS_MESSAGE_TYPE_PERFORMANCE_BIT_EXT, .pfnUserCallback = validation_message, .pUserData = gpu };
		CHECK(vkCreateDebugUtilsMessengerEXT(gpu->instance, &debug, NULL, &gpu->messenger));
	}
	uint32_t device_count = 0;
	CHECK(vkEnumeratePhysicalDevices(gpu->instance, &device_count, NULL));
	VkPhysicalDevice *devices = calloc(device_count ? device_count : 1, sizeof(*devices));
	if (!devices) { fail(error, error_size, "enumerate devices", VK_ERROR_OUT_OF_HOST_MEMORY); goto failure; }
	VkResult enumerate = vkEnumeratePhysicalDevices(gpu->instance, &device_count, devices);
	if (enumerate != VK_SUCCESS) { free(devices); fail(error, error_size, "enumerate devices", enumerate); goto failure; }
	uint32_t family = UINT32_MAX;
	int best_score = -1;
	for (uint32_t device_index = 0; device_index < device_count; device_index++) {
		if (index >= 0 && device_index != (uint32_t)index) { continue; }
		VkPhysicalDeviceProperties properties;
		VkPhysicalDeviceFeatures features;
		vkGetPhysicalDeviceProperties(devices[device_index], &properties);
		vkGetPhysicalDeviceFeatures(devices[device_index], &features);
		if (properties.apiVersion < VK_API_VERSION_1_3 || !features.shaderInt64 || properties.limits.maxComputeWorkGroupInvocations < 64 || properties.limits.maxComputeWorkGroupSize[0] < 64 || properties.limits.maxComputeSharedMemorySize < 5200 || properties.limits.maxComputeWorkGroupCount[0] < streams || properties.limits.maxStorageBufferRange < streams * 152u) { continue; }
		uint32_t queue_count = 0;
		vkGetPhysicalDeviceQueueFamilyProperties(devices[device_index], &queue_count, NULL);
		VkQueueFamilyProperties *queues = calloc(queue_count, sizeof(*queues));
		if (!queues) { continue; }
		vkGetPhysicalDeviceQueueFamilyProperties(devices[device_index], &queue_count, queues);
		for (uint32_t queue_index = 0; queue_index < queue_count; queue_index++) {
			if (!(queues[queue_index].queueFlags & VK_QUEUE_COMPUTE_BIT) || !queues[queue_index].timestampValidBits) { continue; }
			int score = properties.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU ? 2 : 1;
			if (score > best_score) {
				best_score = score;
				gpu->physical = devices[device_index];
				family = queue_index;
				gpu->timestamp_bits = queues[queue_index].timestampValidBits;
				gpu->timestamp_period = properties.limits.timestampPeriod;
				memcpy(gpu->name, properties.deviceName, sizeof(gpu->name));
			}
			break;
		}
		free(queues);
	}
	free(devices);
	if (!gpu->physical) { fail(error, error_size, "selected device requires Vulkan 1.3, shaderInt64, compute timestamps and sufficient storage/workgroup limits", VK_ERROR_FEATURE_NOT_PRESENT); goto failure; }
	float priority = 1;
	VkDeviceQueueCreateInfo queue_info = { .sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO, .queueFamilyIndex = family, .queueCount = 1, .pQueuePriorities = &priority };
	VkPhysicalDeviceFeatures features = { .shaderInt64 = VK_TRUE };
	VkDeviceCreateInfo device = { .sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO, .queueCreateInfoCount = 1, .pQueueCreateInfos = &queue_info, .pEnabledFeatures = &features };
	CHECK(vkCreateDevice(gpu->physical, &device, NULL, &gpu->device));
	volkLoadDevice(gpu->device);
	vkGetDeviceQueue(gpu->device, family, 0, &gpu->queue);
	vkGetPhysicalDeviceMemoryProperties(gpu->physical, &gpu->memory);
	VkMemoryPropertyFlags host = VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT;
	if (!buffer_create(gpu, &gpu->streams, streams * 152u, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, error, error_size)) { goto failure; }
	if (!buffer_create(gpu, &gpu->table, table_size, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, error, error_size)) { goto failure; }
	if (!buffer_create(gpu, &gpu->table_upload, table_size, host, error, error_size)) { goto failure; }
	memcpy(gpu->table_upload.mapped, table, table_size);
	VkDescriptorSetLayoutBinding bindings[4];
	for (uint32_t binding = 0; binding < 4; binding++) { bindings[binding] = (VkDescriptorSetLayoutBinding){ .binding = binding, .descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, .descriptorCount = 1, .stageFlags = VK_SHADER_STAGE_COMPUTE_BIT }; }
	VkDescriptorSetLayoutCreateInfo descriptors = { .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, .bindingCount = 4, .pBindings = bindings };
	CHECK(vkCreateDescriptorSetLayout(gpu->device, &descriptors, NULL, &gpu->descriptors));
	VkPushConstantRange push = { .stageFlags = VK_SHADER_STAGE_COMPUTE_BIT, .size = 16 };
	VkPipelineLayoutCreateInfo layout = { .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO, .setLayoutCount = 1, .pSetLayouts = &gpu->descriptors, .pushConstantRangeCount = 1, .pPushConstantRanges = &push };
	CHECK(vkCreatePipelineLayout(gpu->device, &layout, NULL, &gpu->layout));
	VkShaderModule module = VK_NULL_HANDLE;
	// malloc guarantees the alignment required by pCode, unlike a Go []byte.
	uint32_t *code = malloc(shader_size);
	if (!code) { fail(error, error_size, "allocate shader", VK_ERROR_OUT_OF_HOST_MEMORY); goto failure; }
	memcpy(code, shader, shader_size);
	VkShaderModuleCreateInfo module_info = { .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO, .codeSize = shader_size, .pCode = code };
	VkResult module_result = vkCreateShaderModule(gpu->device, &module_info, NULL, &module);
	free(code);
	if (module_result != VK_SUCCESS) { fail(error, error_size, "create shader module", module_result); goto failure; }
	VkComputePipelineCreateInfo pipeline = { .sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO, .stage = { .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, .stage = VK_SHADER_STAGE_COMPUTE_BIT, .module = module, .pName = "main" }, .layout = gpu->layout };
	VkResult pipeline_result = vkCreateComputePipelines(gpu->device, VK_NULL_HANDLE, 1, &pipeline, NULL, &gpu->pipeline);
	vkDestroyShaderModule(gpu->device, module, NULL);
	if (pipeline_result != VK_SUCCESS) { fail(error, error_size, "create compute pipeline", pipeline_result); goto failure; }
	VkDescriptorPoolSize pool_size = { .type = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, .descriptorCount = 8 };
	VkDescriptorPoolCreateInfo pool = { .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, .maxSets = 2, .poolSizeCount = 1, .pPoolSizes = &pool_size };
	CHECK(vkCreateDescriptorPool(gpu->device, &pool, NULL, &gpu->descriptor_pool));
	VkCommandPoolCreateInfo command_pool = { .sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO, .flags = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT, .queueFamilyIndex = family };
	CHECK(vkCreateCommandPool(gpu->device, &command_pool, NULL, &gpu->command_pool));
	VkQueryPoolCreateInfo queries = { .sType = VK_STRUCTURE_TYPE_QUERY_POOL_CREATE_INFO, .queryType = VK_QUERY_TYPE_TIMESTAMP, .queryCount = 4 };
	CHECK(vkCreateQueryPool(gpu->device, &queries, NULL, &gpu->queries));
	for (uint32_t slot_index = 0; slot_index < 2; slot_index++) {
		onino_slot *slot = &gpu->slots[slot_index];
		if (!buffer_create(gpu, &slot->commands, streams * sizeof(onino_command), host, error, error_size)) { goto failure; }
		if (!buffer_create(gpu, &slot->results, 16 + capacity * sizeof(onino_hit), host, error, error_size)) { goto failure; }
		VkDescriptorSetAllocateInfo allocate = { .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, .descriptorPool = gpu->descriptor_pool, .descriptorSetCount = 1, .pSetLayouts = &gpu->descriptors };
		CHECK(vkAllocateDescriptorSets(gpu->device, &allocate, &slot->descriptors));
		onino_buffer *buffers[4] = { &gpu->streams, &gpu->table, &slot->commands, &slot->results };
		for (uint32_t binding = 0; binding < 4; binding++) {
			VkDescriptorBufferInfo buffer = { .buffer = buffers[binding]->buffer, .range = VK_WHOLE_SIZE };
			VkWriteDescriptorSet write = { .sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, .dstSet = slot->descriptors, .dstBinding = binding, .descriptorCount = 1, .descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, .pBufferInfo = &buffer };
			vkUpdateDescriptorSets(gpu->device, 1, &write, 0, NULL);
		}
		VkCommandBufferAllocateInfo command = { .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO, .commandPool = gpu->command_pool, .level = VK_COMMAND_BUFFER_LEVEL_PRIMARY, .commandBufferCount = 1 };
		CHECK(vkAllocateCommandBuffers(gpu->device, &command, &slot->command));
		VkFenceCreateInfo fence = { .sType = VK_STRUCTURE_TYPE_FENCE_CREATE_INFO };
		CHECK(vkCreateFence(gpu->device, &fence, NULL, &slot->fence));
	}
	// One startup-only clear, ordered before every search on the same queue.
	VkCommandBuffer command = gpu->slots[0].command;
	VkCommandBufferBeginInfo begin = { .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
	CHECK(vkBeginCommandBuffer(command, &begin));
	vkCmdFillBuffer(command, gpu->streams.buffer, 0, VK_WHOLE_SIZE, 0);
	VkBufferCopy table_copy = { .size = table_size };
	vkCmdCopyBuffer(command, gpu->table_upload.buffer, gpu->table.buffer, 1, &table_copy);
	CHECK(vkEndCommandBuffer(command));
	VkSubmitInfo submit = { .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO, .commandBufferCount = 1, .pCommandBuffers = &command };
	CHECK(vkQueueSubmit(gpu->queue, 1, &submit, gpu->slots[0].fence));
	CHECK(vkWaitForFences(gpu->device, 1, &gpu->slots[0].fence, VK_TRUE, UINT64_MAX));
	buffer_destroy(gpu, &gpu->table_upload);
	return gpu;
failure:
	onino_close(gpu);
	return NULL;
}

const char *onino_name(onino_gpu *gpu) { return gpu->name; }
uint32_t onino_validation_errors(onino_gpu *gpu) { return atomic_load(&gpu->validation_errors); }

int onino_submit(onino_gpu *gpu, uint32_t slot_index, const onino_command *commands, uint32_t rounds, int collect_only, char *error, size_t error_size) {
	if (slot_index >= 2 || gpu->slots[slot_index].submitted) { return fail(error, error_size, "submission slot still in flight", VK_NOT_READY); }
	onino_slot *slot = &gpu->slots[slot_index];
	memcpy(slot->commands.mapped, commands, gpu->stream_count * sizeof(*commands));
	CHECK(vkResetFences(gpu->device, 1, &slot->fence));
	CHECK(vkResetCommandBuffer(slot->command, 0));
	VkCommandBufferBeginInfo begin = { .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO, .flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT };
	CHECK(vkBeginCommandBuffer(slot->command, &begin));
	VkMemoryBarrier before = { .sType = VK_STRUCTURE_TYPE_MEMORY_BARRIER, .srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT | VK_ACCESS_TRANSFER_WRITE_BIT | VK_ACCESS_HOST_WRITE_BIT, .dstAccessMask = VK_ACCESS_SHADER_READ_BIT | VK_ACCESS_SHADER_WRITE_BIT | VK_ACCESS_TRANSFER_WRITE_BIT };
	vkCmdPipelineBarrier(slot->command, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT | VK_PIPELINE_STAGE_TRANSFER_BIT | VK_PIPELINE_STAGE_HOST_BIT, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT | VK_PIPELINE_STAGE_TRANSFER_BIT, 0, 1, &before, 0, NULL, 0, NULL);
	vkCmdFillBuffer(slot->command, slot->results.buffer, 0, 16, 0);
	VkMemoryBarrier clear = { .sType = VK_STRUCTURE_TYPE_MEMORY_BARRIER, .srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT, .dstAccessMask = VK_ACCESS_SHADER_READ_BIT | VK_ACCESS_SHADER_WRITE_BIT };
	vkCmdPipelineBarrier(slot->command, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, 0, 1, &clear, 0, NULL, 0, NULL);
	vkCmdResetQueryPool(slot->command, gpu->queries, slot_index * 2, 2);
	vkCmdWriteTimestamp(slot->command, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, gpu->queries, slot_index * 2);
	vkCmdBindPipeline(slot->command, VK_PIPELINE_BIND_POINT_COMPUTE, gpu->pipeline);
	vkCmdBindDescriptorSets(slot->command, VK_PIPELINE_BIND_POINT_COMPUTE, gpu->layout, 0, 1, &slot->descriptors, 0, NULL);
	uint32_t parameters[4] = { rounds, gpu->capacity, collect_only != 0, 0 };
	vkCmdPushConstants(slot->command, gpu->layout, VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof(parameters), parameters);
	vkCmdDispatch(slot->command, gpu->stream_count, 1, 1);
	vkCmdWriteTimestamp(slot->command, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, gpu->queries, slot_index * 2 + 1);
	VkMemoryBarrier after = { .sType = VK_STRUCTURE_TYPE_MEMORY_BARRIER, .srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT, .dstAccessMask = VK_ACCESS_HOST_READ_BIT };
	vkCmdPipelineBarrier(slot->command, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_HOST_BIT, 0, 1, &after, 0, NULL, 0, NULL);
	CHECK(vkEndCommandBuffer(slot->command));
	VkSubmitInfo submit = { .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO, .commandBufferCount = 1, .pCommandBuffers = &slot->command };
	CHECK(vkQueueSubmit(gpu->queue, 1, &submit, slot->fence));
	slot->submitted = 1;
	return 1;
failure:
	return 0;
}
int onino_collect(onino_gpu *gpu, uint32_t slot_index, const onino_result **result, double *nanoseconds, double *gap, char *error, size_t error_size) {
	if (slot_index >= 2 || !gpu->slots[slot_index].submitted) { return fail(error, error_size, "submission slot is not in flight", VK_NOT_READY); }
	onino_slot *slot = &gpu->slots[slot_index];
	CHECK(vkWaitForFences(gpu->device, 1, &slot->fence, VK_TRUE, UINT64_MAX));
	uint64_t timestamps[2];
	CHECK(vkGetQueryPoolResults(gpu->device, gpu->queries, slot_index * 2, 2, sizeof(timestamps), timestamps, sizeof(uint64_t), VK_QUERY_RESULT_64_BIT));
	uint64_t ticks = timestamps[1] - timestamps[0];
	uint64_t gap_ticks = gpu->have_timestamp ? timestamps[0] - gpu->last_timestamp : 0;
	if (gpu->timestamp_bits < 64) { ticks &= (UINT64_C(1) << gpu->timestamp_bits) - 1; }
	if (gpu->timestamp_bits < 64) { gap_ticks &= (UINT64_C(1) << gpu->timestamp_bits) - 1; }
	*nanoseconds = (double)ticks * gpu->timestamp_period;
	*gap = (double)gap_ticks * gpu->timestamp_period;
	gpu->last_timestamp = timestamps[1];
	gpu->have_timestamp = 1;
	*result = slot->results.mapped;
	slot->submitted = 0;
	return 1;
failure:
	return 0;
}
void onino_close(onino_gpu *gpu) {
	if (!gpu) { return; }
	if (gpu->device) {
		// Shutdown and failed initialization only; never in the steady-state loop.
		vkDeviceWaitIdle(gpu->device);
		for (uint32_t index = 0; index < 2; index++) {
			buffer_destroy(gpu, &gpu->slots[index].commands);
			buffer_destroy(gpu, &gpu->slots[index].results);
			if (gpu->slots[index].fence) { vkDestroyFence(gpu->device, gpu->slots[index].fence, NULL); }
		}
		buffer_destroy(gpu, &gpu->streams);
		buffer_destroy(gpu, &gpu->table);
		buffer_destroy(gpu, &gpu->table_upload);
		if (gpu->queries) { vkDestroyQueryPool(gpu->device, gpu->queries, NULL); }
		if (gpu->command_pool) { vkDestroyCommandPool(gpu->device, gpu->command_pool, NULL); }
		if (gpu->descriptor_pool) { vkDestroyDescriptorPool(gpu->device, gpu->descriptor_pool, NULL); }
		if (gpu->pipeline) { vkDestroyPipeline(gpu->device, gpu->pipeline, NULL); }
		if (gpu->layout) { vkDestroyPipelineLayout(gpu->device, gpu->layout, NULL); }
		if (gpu->descriptors) { vkDestroyDescriptorSetLayout(gpu->device, gpu->descriptors, NULL); }
		vkDestroyDevice(gpu->device, NULL);
	}
	if (gpu->messenger) { vkDestroyDebugUtilsMessengerEXT(gpu->instance, gpu->messenger, NULL); }
	if (gpu->instance) { vkDestroyInstance(gpu->instance, NULL); }
	volkFinalize();
	free(gpu);
}
