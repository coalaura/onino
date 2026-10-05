package cpu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func Discover() (Topology, error) {
	mask, err := affinity()
	if err != nil {
		return Topology{}, fmt.Errorf("read allowed CPU mask: %w", err)
	}

	topology := Topology{Known: true}

	cores := make(map[string]int)
	caches := make(map[string]int)

	for number := 0; number < len(mask)*8; number++ {
		if mask[number/8]&(1<<uint(number%8)) == 0 {
			continue
		}

		base := fmt.Sprintf("/sys/devices/system/cpu/cpu%d", number)

		packageID, packageError := os.ReadFile(filepath.Join(base, "topology/physical_package_id"))
		coreID, coreError := os.ReadFile(filepath.Join(base, "topology/core_id"))

		cacheID, cacheError := lastCache(base)

		if packageError != nil || coreError != nil || cacheError != nil {
			topology.Known = false
		}

		core := string(packageID) + ":" + string(coreID)
		processor := CPU{Number: number, Core: identifier(cores, core), Cache: identifier(caches, cacheID)}
		topology.CPUs = append(topology.CPUs, processor)
	}

	return topology, nil
}

func pin(processor CPU) (threadAffinity, error) {
	previous, err := affinity()
	if err != nil {
		return threadAffinity{}, err
	}

	number := processor.Number
	if processor.Group != 0 || number < 0 || number >= len(previous)*8 || previous[number/8]&(1<<uint(number%8)) == 0 {
		return threadAffinity{}, errors.New("processor is outside the allowed CPU mask")
	}

	mask := make([]byte, len(previous))
	mask[number/8] = 1 << uint(number%8)

	err = setAffinity(mask)
	if err != nil {
		return threadAffinity{}, err
	}

	restore := func() error {
		return setAffinity(previous)
	}

	return threadAffinity{restore: restore}, nil
}

func affinity() ([]byte, error) {
	for size := 128; size <= 1<<20; size *= 2 {
		mask := make([]byte, size)

		length, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_GETAFFINITY, 0, uintptr(size), uintptr(unsafe.Pointer(&mask[0])))
		if errno == syscall.EINVAL {
			continue
		}

		if errno != 0 {
			return nil, errno
		}

		return mask[:length], nil
	}

	return nil, errors.New("CPU affinity mask exceeds supported size")
}

func setAffinity(mask []byte) error {
	_, _, errno := syscall.RawSyscall(syscall.SYS_SCHED_SETAFFINITY, 0, uintptr(len(mask)), uintptr(unsafe.Pointer(&mask[0])))
	if errno != 0 {
		return errno
	}

	return nil
}

func lastCache(base string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(base, "cache/index*"))
	if err != nil {
		return "", err
	}

	var (
		selected string
		highest  int
	)

	for _, path := range paths {
		data, readError := os.ReadFile(filepath.Join(path, "level"))
		if readError != nil {
			return "", readError
		}

		level, parseError := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseError != nil {
			return "", parseError
		}

		if level > highest {
			highest = level
			selected = path
		}
	}

	data, err := os.ReadFile(filepath.Join(selected, "shared_cpu_list"))

	return string(data), err
}

func identifier(identifiers map[string]int, key string) int {
	value, exists := identifiers[key]
	if !exists {
		value = len(identifiers)

		identifiers[key] = value
	}

	return value
}
