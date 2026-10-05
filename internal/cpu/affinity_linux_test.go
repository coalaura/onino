package cpu

import (
	"bytes"
	"runtime"
	"testing"
)

func TestAffinityRestore(t *testing.T) {
	topology, err := Discover()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		before, readError := affinity()
		if readError != nil {
			done <- readError

			return
		}

		restore, pinError := Pin(topology.CPUs[len(topology.CPUs)-1])
		if pinError != nil {
			done <- pinError

			return
		}

		during, duringError := affinity()

		want := make([]byte, len(before))

		number := topology.CPUs[len(topology.CPUs)-1].Number
		want[number/8] = 1 << uint(number%8)

		if duringError != nil || !bytes.Equal(during, want) {
			t.Error("worker was not pinned to the requested CPU")
		}

		restricted, discoveryError := Discover()
		if discoveryError != nil || len(restricted.CPUs) != 1 || restricted.CPUs[0].Number != number {
			t.Error("discovery did not respect the allowed CPU mask")
		}

		restoreError := restore()
		if restoreError != nil {
			done <- restoreError

			return
		}

		after, readError := affinity()
		if !bytes.Equal(before, after) {
			t.Error("affinity was not restored")
		}

		done <- readError
	}()

	err = <-done
	if err != nil {
		t.Fatal(err)
	}
}
