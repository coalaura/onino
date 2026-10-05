//go:build !linux && !windows

package cpu

import (
	"errors"
	"runtime"
)

// Discover uses the runtime's process-available count on portable targets.
// Physical topology and affinity are explicitly unsupported there.
func Discover() (Topology, error) {
	return Topology{CPUs: make([]CPU, runtime.NumCPU())}, nil
}

func pin(CPU) (threadAffinity, error) {
	return threadAffinity{}, errors.New("thread affinity is unsupported on this platform")
}
