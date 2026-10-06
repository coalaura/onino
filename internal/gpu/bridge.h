#ifndef ONINO_GPU_BRIDGE_H
#define ONINO_GPU_BRIDGE_H
#include <stdint.h>
#include <stddef.h>

typedef struct onino_gpu onino_gpu;
typedef struct { uint32_t center[30], expected, generation, action; } onino_command;
typedef struct { uint32_t stream, generation, steps, kind, low, high; } onino_hit;
typedef struct {
	uint32_t count, checked, pending, reserved;
	onino_hit hits[];
} onino_result;
onino_gpu *onino_open(int index, int validation, uint32_t streams, uint32_t capacity, const void *shader, size_t shader_size, const void *table, size_t table_size, char *error, size_t error_size);
const char *onino_name(onino_gpu *gpu);
int onino_submit(onino_gpu *gpu, uint32_t slot, const onino_command *commands, uint32_t rounds, int collect_only, char *error, size_t error_size);
int onino_collect(onino_gpu *gpu, uint32_t slot, const onino_result **result, double *nanoseconds, double *gap, char *error, size_t error_size);
uint32_t onino_validation_errors(onino_gpu *gpu);
void onino_close(onino_gpu *gpu);
#endif
