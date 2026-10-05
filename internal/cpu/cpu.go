// Package cpu isolates process CPU availability, topology and thread affinity.
package cpu

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
)

// CPU identifies a logical processor and its physical core and last-level cache.
// Core and Cache identifiers are unique across processor groups.
type CPU struct {
	Group  uint16
	Number int
	Core   int
	Cache  int
}

type Topology struct {
	CPUs  []CPU
	Known bool
}

type threadAffinity struct {
	restore func() error
	retire  bool
}

func (affinity threadAffinity) release(unlock func()) error {
	err := affinity.restore()
	if err == nil && !affinity.retire {
		unlock()
	}

	return err
}

// Resolve accepts a strict positive decimal count or "all".
func Resolve(value string, available int) (int, error) {
	if available < 1 {
		return 0, errors.New("no processors available to this process")
	}

	if value == "all" {
		return available, nil
	}

	for _, symbol := range value {
		if symbol < '0' || symbol > '9' {
			return 0, errors.New("--cpu must be a positive integer or all")
		}
	}

	count, err := strconv.Atoi(value)
	if err != nil || count < 1 || count > available {
		return 0, fmt.Errorf("--cpu must be between 1 and %d, or all", available)
	}

	return count, nil
}

// Select visits distinct physical cores before SMT siblings. Spread alternates
// last-level caches; otherwise the discovered cache order is kept together.
func Select(topology Topology, count int, spread bool) ([]CPU, error) {
	if !topology.Known {
		return nil, errors.New("physical CPU topology is unavailable")
	}

	if count < 1 || count > len(topology.CPUs) {
		return nil, errors.New("invalid CPU placement count")
	}

	caches := make([]int, 0, len(topology.CPUs))
	groups := make(map[int][]CPU, len(topology.CPUs))

	for _, processor := range topology.CPUs {
		if _, exists := groups[processor.Cache]; !exists {
			caches = append(caches, processor.Cache)
		}

		groups[processor.Cache] = append(groups[processor.Cache], processor)
	}

	selected := make([]CPU, 0, count)
	used := make(map[CPU]bool, count)
	cores := make(map[int]int, count)

	for sibling := 0; len(selected) < count; sibling++ {
		for {
			added := false

			for _, cache := range caches {
				for _, processor := range groups[cache] {
					if used[processor] || cores[processor.Core] != sibling {
						continue
					}

					selected = append(selected, processor)
					used[processor] = true
					cores[processor.Core]++
					added = true

					if len(selected) == count {
						return selected, nil
					}

					if spread {
						break
					}
				}
			}

			if !added {
				break
			}
		}
	}

	return selected, nil
}

// Pin locks the calling goroutine to its OS thread before changing affinity.
// The returned cleanup must run on that goroutine. On restoration failure the
// thread stays locked: returning from the goroutine retires it, rather than
// returning an incorrectly constrained thread to the runtime's pool.
func Pin(processor CPU) (func() error, error) {
	runtime.LockOSThread()

	affinity, err := pin(processor)
	if err != nil {
		runtime.UnlockOSThread()

		return nil, err
	}

	return func() error {
		return affinity.release(runtime.UnlockOSThread)
	}, nil
}
