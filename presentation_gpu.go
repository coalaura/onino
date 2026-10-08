//go:build gpu

package main

import (
	"fmt"
	"time"
)

func (reporter *presentation) initializedGPU(device string, initialization time.Duration) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	reporter.setup.gpu.device = device
	reporter.setup.gpu.initialization = initialization
	reporter.phase = "tuning"

	message := "Tuning GPU; calibration performs useful search.\n"

	if !reporter.setup.gpu.autoStreams && !reporter.setup.gpu.autoRounds {
		reporter.phase = "validating"
		message = "Validating GPU settings with useful search.\n"
	}

	reporter.write(reporter.stderr, []byte(message))
}

func (reporter *presentation) readyGPU(device string) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	reporter.setup.gpu.first = time.Since(reporter.started)
}

func (reporter *presentation) selectedGPU(device string, streams, rounds int, first, selected time.Duration) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	reporter.setup.gpu.device = device
	reporter.setup.gpu.streams = streams
	reporter.setup.gpu.rounds = rounds
	reporter.setup.gpu.first = first
	reporter.setup.gpu.selection = selected
	reporter.phase = ""
	reporter.printSetup()
}

func (reporter *presentation) diagnostic(message string) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	line := fmt.Appendf(reporter.buffer[:0], "GPU diagnostic: %s\n", message)
	reporter.write(reporter.stderr, line)
}
