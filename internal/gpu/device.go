//go:build gpu

// Package gpu implements the optional Vulkan prefix engine. Private scalar material never crosses its C boundary.
package gpu

/*
#cgo CFLAGS: -std=c11 -I${SRCDIR}/vendor
#cgo linux LDFLAGS: -ldl
#include "bridge.h"
*/
import "C"

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

type command struct {
	Center     [30]uint32
	Expected   uint32
	Generation uint32
	Action     uint32
}

type hit struct {
	Stream     uint32
	Generation uint32
	Steps      uint32
	Kind       uint32
	Low        uint32
	High       uint32
}

type device struct {
	handle   *C.onino_gpu
	name     string
	capacity int
	streams  int
	message  [1024]C.char
	result   *C.onino_result
	elapsed  C.double
	gap      C.double
	cost     C.onino_cost
}

type collection struct {
	hits    []hit
	checked uint32
	pending uint32
	elapsed time.Duration
	gap     time.Duration
}

var (
	//go:embed shaders/search.spv
	searchShader []byte
	bridgeMutex  sync.Mutex
)

func (engine *device) close() {
	C.onino_close(engine.handle)

	bridgeMutex.Unlock()
}

func (engine *device) submit(slot int, commands []command, rounds int, collectOnly bool) error {
	return engine.dispatch(slot, commands, 0, len(commands), rounds, collectOnly)
}

func (engine *device) dispatch(slot int, commands []command, first, active, rounds int, collectOnly bool) error {
	collectFlag := C.int(0)
	if collectOnly {
		collectFlag = 1
	}

	success := C.onino_submit(engine.handle, C.uint32_t(slot), (*C.onino_command)(unsafe.Pointer(unsafe.SliceData(commands))), C.uint32_t(first), C.uint32_t(len(commands)), C.uint32_t(active), C.uint32_t(rounds), collectFlag, &engine.message[0], C.size_t(len(engine.message)))
	if success == 0 {
		return errors.New(C.GoString(&engine.message[0]))
	}

	return nil
}

func (engine *device) collect(slot int) (collection, error) {
	// Reuse the CGO out-parameters instead of escaping new Go locals on every collection.
	success := C.onino_collect(engine.handle, C.uint32_t(slot), &engine.result, &engine.elapsed, &engine.gap, &engine.message[0], C.size_t(len(engine.message)))
	if success == 0 {
		return collection{}, errors.New(C.GoString(&engine.message[0]))
	}

	result := engine.result
	count := min(int(result.count), engine.capacity)
	hits := unsafe.Slice((*hit)(unsafe.Add(unsafe.Pointer(result), 16)), count)

	// The view is C-owned and remains valid until this slot is resubmitted.
	return collection{hits: hits, checked: uint32(result.checked), pending: uint32(result.pending), elapsed: time.Duration(engine.elapsed), gap: time.Duration(engine.gap)}, nil
}

func (engine *device) validationErrors() uint32 {
	return uint32(C.onino_validation_errors(engine.handle))
}

func (engine *device) memory() string {
	C.onino_memory(engine.handle, &engine.message[0], C.size_t(len(engine.message)))

	return C.GoString(&engine.message[0])
}

func (engine *device) diagnostics(report func(string)) {
	C.onino_diagnostics(engine.handle)
	C.onino_costs(engine.handle, &engine.cost)

	report(fmt.Sprintf("initialization_ms=%.3f pipeline_ms=%.3f", float64(engine.cost.initialize_ns)/1e6, float64(engine.cost.pipeline_ns)/1e6))
}

func (engine *device) costs(metrics *Metrics) {
	C.onino_costs(engine.handle, &engine.cost)

	metrics.Copy = time.Duration(engine.cost.copy_ns)
	metrics.Record = time.Duration(engine.cost.record_ns)
	metrics.Submit = time.Duration(engine.cost.submit_ns)
	metrics.UploadBytes = uint64(engine.cost.copied_bytes)
	metrics.Recordings = uint64(engine.cost.recordings)
}

func openDevice(index int, validation bool, streams, capacity int, table []uint32, shader []byte) (*device, error) {
	// volk's dispatch pointers are process-global. Only one selected device is supported.
	locked := bridgeMutex.TryLock()
	if !locked {
		return nil, errors.New("a Vulkan engine is already open")
	}

	var message [512]C.char

	validationFlag := C.int(0)
	if validation {
		validationFlag = 1
	}

	handle := C.onino_open(C.int(index), validationFlag, C.uint32_t(streams), C.uint32_t(capacity), unsafe.Pointer(&shader[0]), C.size_t(len(shader)), unsafe.Pointer(&table[0]), C.size_t(len(table)*4), &message[0], C.size_t(len(message)))
	if handle == nil {
		bridgeMutex.Unlock()

		return nil, fmt.Errorf("open Vulkan: %s", C.GoString(&message[0]))
	}

	return &device{handle: handle, name: C.GoString(C.onino_name(handle)), capacity: capacity, streams: int(C.onino_streams(handle))}, nil
}
