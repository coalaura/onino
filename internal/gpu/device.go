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
	message  [512]C.char
	result   *C.onino_result
	elapsed  C.double
	gap      C.double
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
	collectFlag := C.int(0)
	if collectOnly {
		collectFlag = 1
	}

	success := C.onino_submit(engine.handle, C.uint32_t(slot), (*C.onino_command)(unsafe.Pointer(&commands[0])), C.uint32_t(rounds), collectFlag, &engine.message[0], C.size_t(len(engine.message)))
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

	return &device{handle: handle, name: C.GoString(C.onino_name(handle)), capacity: capacity}, nil
}
