package cpu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"syscall"
	"unsafe"
)

type groupAffinity struct {
	Mask     uintptr
	Group    uint16
	Reserved [3]uint16
}

var (
	kernel               = syscall.NewLazyDLL("kernel32.dll")
	getCPUSetInformation = kernel.NewProc("GetSystemCpuSetInformation")
	getDefaultCPUSets    = kernel.NewProc("GetProcessDefaultCpuSets")
	getProcessAffinity   = kernel.NewProc("GetProcessAffinityMask")
	getProcessGroups     = kernel.NewProc("GetProcessGroupAffinity")
	setThreadAffinity    = kernel.NewProc("SetThreadGroupAffinity")
	getVersion           = syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	getGroupCount        = kernel.NewProc("GetActiveProcessorGroupCount")
)

func Discover() (Topology, error) {
	err := getCPUSetInformation.Find()
	if err != nil {
		return legacyTopology()
	}

	process := ^uintptr(0)

	var length uint32

	getCPUSetInformation.Call(0, 0, uintptr(unsafe.Pointer(&length)), process, 0)

	if length == 0 {
		return Topology{}, errors.New("CPU set information is unavailable")
	}

	data := make([]byte, length)

	success, _, callError := getCPUSetInformation.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(length), uintptr(unsafe.Pointer(&length)), process, 0)
	if success == 0 {
		return Topology{}, fmt.Errorf("read CPU sets: %w", callError)
	}

	allowed, err := defaultCPUSets(process)
	if err != nil {
		return Topology{}, err
	}

	groups, err := processGroups(process)
	if err != nil {
		return Topology{}, err
	}

	var (
		processMask uintptr
		systemMask  uintptr
	)

	success, _, callError = getProcessAffinity.Call(process, uintptr(unsafe.Pointer(&processMask)), uintptr(unsafe.Pointer(&systemMask)))
	if success == 0 {
		return Topology{}, fmt.Errorf("read process affinity: %w", callError)
	}

	topology := Topology{Known: true}

	// Windows 11 / Server 2022 schedule across groups by default, even when
	// the process currently has threads in only its primary group. The mask
	// query then describes that primary group, not total CPU availability.
	allGroups := defaultAllGroups() && processMask == systemMask

	for offset := 0; offset+8 <= int(length); {
		size := int(binary.LittleEndian.Uint32(data[offset:]))
		kind := binary.LittleEndian.Uint32(data[offset+4:])

		if size < 8 || offset+size > int(length) {
			return Topology{}, errors.New("invalid CPU set information")
		}

		record := data[offset : offset+size]
		offset += size

		if kind != 0 || size < 32 {
			continue
		}

		id := binary.LittleEndian.Uint32(record[8:])
		group := binary.LittleEndian.Uint16(record[12:])

		number := int(record[14])

		flags := record[19]
		// CPU sets allocated to another process are not available to us.
		if flags&2 != 0 && flags&4 == 0 {
			continue
		}

		if len(allowed) != 0 && !allowed[id] {
			continue
		}

		// A single-group process has a meaningful hard affinity mask. On
		// Windows 11 a multi-group process can use all groups by default.
		if len(groups) == 1 && !allGroups && (group != groups[0] || processMask&(uintptr(1)<<uint(number)) == 0) {
			continue
		}

		processor := CPU{
			Group:  group,
			Number: number,
			Core:   int(group)*256 + int(record[15]),
			Cache:  int(group)*256 + int(record[16]),
		}

		topology.CPUs = append(topology.CPUs, processor)
	}

	return topology, nil
}

func pin(processor CPU) (threadAffinity, error) {
	if processor.Number < 0 || processor.Number >= bits.UintSize {
		return threadAffinity{}, errors.New("processor number exceeds affinity mask width")
	}

	requested := groupAffinity{Mask: uintptr(1) << uint(processor.Number), Group: processor.Group}

	var previous groupAffinity

	success, _, err := setThreadAffinity.Call(^uintptr(1), uintptr(unsafe.Pointer(&requested)), uintptr(unsafe.Pointer(&previous)))
	if success == 0 {
		return threadAffinity{}, fmt.Errorf("set thread group affinity: %w", err)
	}

	restore := func() error {
		success, _, err := setThreadAffinity.Call(^uintptr(1), uintptr(unsafe.Pointer(&previous)), 0)
		if success == 0 {
			return fmt.Errorf("restore thread group affinity: %w", err)
		}

		return nil
	}

	// GROUP_AFFINITY cannot represent Windows 11's unconstrained multi-group
	// default. Restore the recorded mask, then retire such threads conservatively
	// instead of returning a possibly narrower affinity to the runtime pool.
	groups, _, _ := getGroupCount.Call()

	return threadAffinity{restore: restore, retire: groups > 1}, nil
}

func defaultCPUSets(process uintptr) (map[uint32]bool, error) {
	var count uint32

	success, _, err := getDefaultCPUSets.Call(process, 0, 0, uintptr(unsafe.Pointer(&count)))

	if count == 0 {
		if success == 0 {
			return nil, fmt.Errorf("read default CPU sets: %w", err)
		}

		return nil, nil
	}

	ids := make([]uint32, count)

	success, _, err = getDefaultCPUSets.Call(process, uintptr(unsafe.Pointer(&ids[0])), uintptr(count), uintptr(unsafe.Pointer(&count)))
	if success == 0 {
		return nil, fmt.Errorf("read default CPU sets: %w", err)
	}

	allowed := make(map[uint32]bool, count)

	for _, id := range ids {
		allowed[id] = true
	}

	return allowed, nil
}

func processGroups(process uintptr) ([]uint16, error) {
	var count uint16

	getProcessGroups.Call(process, uintptr(unsafe.Pointer(&count)), 0)

	if count == 0 {
		return nil, errors.New("process processor groups are unavailable")
	}

	groups := make([]uint16, count)

	success, _, err := getProcessGroups.Call(process, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&groups[0])))
	if success == 0 {
		return nil, fmt.Errorf("read process processor groups: %w", err)
	}

	return groups[:count], nil
}

func legacyTopology() (Topology, error) {
	var (
		processMask uintptr
		systemMask  uintptr
	)

	success, _, err := getProcessAffinity.Call(^uintptr(0), uintptr(unsafe.Pointer(&processMask)), uintptr(unsafe.Pointer(&systemMask)))
	if success == 0 || processMask == 0 {
		return Topology{}, fmt.Errorf("read legacy process affinity: %w", err)
	}

	return Topology{CPUs: make([]CPU, bits.OnesCount(uint(processMask)))}, nil
}

func defaultAllGroups() bool {
	var version [284]byte

	binary.LittleEndian.PutUint32(version[:], uint32(len(version)))

	status, _, _ := getVersion.Call(uintptr(unsafe.Pointer(&version[0])))
	if status != 0 {
		return false
	}

	build := binary.LittleEndian.Uint32(version[12:])

	product := version[282]

	return build >= 22000 || (product != 1 && build >= 20348)
}
