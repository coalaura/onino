package cpu

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"unsafe"
)

func TestRestrictedProcess(t *testing.T) {
	if os.Getenv("ONINO_AFFINITY_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestRestrictedProcess$")
		command.Env = append(os.Environ(), "ONINO_AFFINITY_CHILD=1")

		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("restricted child: %v\n%s", err, output)
		}

		return
	}

	var (
		processMask uintptr
		systemMask  uintptr
	)

	success, _, err := getProcessAffinity.Call(^uintptr(0), uintptr(unsafe.Pointer(&processMask)), uintptr(unsafe.Pointer(&systemMask)))
	if success == 0 || processMask == 0 {
		t.Fatalf("process mask: %v", err)
	}

	// Isolate the lowest available bit, rather than assuming CPU zero is allowed.
	mask := processMask & -processMask

	setProcessAffinity := kernel.NewProc("SetProcessAffinityMask")

	success, _, err = setProcessAffinity.Call(^uintptr(0), mask)
	if success == 0 {
		t.Fatal(err)
	}

	topology, discoveryError := Discover()
	if discoveryError != nil || len(topology.CPUs) != 1 {
		t.Fatalf("restricted discovery: %+v %v", topology, discoveryError)
	}

	count, resolveError := Resolve("all", len(topology.CPUs))
	if resolveError != nil || count != 1 {
		t.Fatalf("restricted all: %d %v", count, resolveError)
	}
}

func TestAffinityRestore(t *testing.T) {
	topology, err := Discover()
	if err != nil {
		t.Fatal(err)
	}

	if !topology.Known {
		t.Skip("physical topology unavailable")
	}

	done := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		getAffinity := kernel.NewProc("GetThreadGroupAffinity")

		var before groupAffinity

		getAffinity.Call(^uintptr(1), uintptr(unsafe.Pointer(&before)))

		restore, pinError := Pin(topology.CPUs[len(topology.CPUs)-1])
		if pinError != nil {
			done <- pinError

			return
		}

		var during groupAffinity

		getAffinity.Call(^uintptr(1), uintptr(unsafe.Pointer(&during)))

		if during.Mask != uintptr(1)<<uint(topology.CPUs[len(topology.CPUs)-1].Number) {
			t.Error("worker was not pinned")
		}

		restoreError := restore()
		if restoreError != nil {
			done <- restoreError

			return
		}

		var after groupAffinity

		getAffinity.Call(^uintptr(1), uintptr(unsafe.Pointer(&after)))

		if before != after {
			t.Error("affinity was not restored")
		}

		done <- nil
	}()

	err = <-done
	if err != nil {
		t.Fatal(err)
	}
}
