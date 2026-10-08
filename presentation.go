package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/search"
)

type searchSetup struct {
	input     patternInput
	workers   int
	placement string
	output    string
	prepared  time.Duration
	gpu       gpuSetup
	config    search.Configuration
}

// presentation owns both streams and all lifecycle output. Its lock serializes
// whole logical blocks, even when stdout and stderr share a redirected writer.
type presentation struct {
	mutex       sync.Mutex
	stdout      io.Writer
	stderr      io.Writer
	started     time.Time
	lastMatch   time.Time
	setup       searchSetup
	estimate    matchEstimate
	monitor     *search.Monitor
	previous    progressSample
	phase       string
	configured  bool
	done        chan struct{}
	finishOnce  sync.Once
	cancel      context.CancelFunc
	outputError error
	resultError error
	buffer      [512]byte
}

type reportedError struct {
	err error
}

func (err reportedError) Error() string {
	return err.err.Error()
}

func (err reportedError) Unwrap() error {
	return err.err
}

func (reporter *presentation) loading(filename string) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	line := fmt.Appendf(reporter.buffer[:0], "Loading patterns from %s...\n", filename)
	reporter.write(reporter.stderr, line)
}

func (reporter *presentation) start(ctx context.Context, setup searchSetup, estimate matchEstimate, monitor *search.Monitor) {
	reporter.setup = setup
	reporter.estimate = estimate
	reporter.monitor = monitor
	reporter.previous = progressSample{time: reporter.started}
	reporter.done = make(chan struct{})

	if setup.gpu.state == backendEnabled {
		reporter.phase = "GPU init"

		if setup.workers != 0 && setup.config.Forced() {
			reporter.printSetup()
		}

		reporter.write(reporter.stderr, []byte("Initializing GPU...\n"))
	} else {
		reporter.printSetup()
	}

	go reporter.run(ctx)
}

func (reporter *presentation) saved(key onion.Key, found time.Time) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	// Save callbacks are serialized, but discovery times can arrive out of order.
	gap := max(0, found.Sub(reporter.lastMatch).Seconds())
	elapsed := max(0, found.Sub(reporter.started).Seconds())

	line := fmt.Appendf(reporter.buffer[:0], "%s saved (%.2fs since previous; %.2fs elapsed)\n", key.Hostname(), gap, elapsed)

	reporter.write(reporter.stdout, line)

	if found.After(reporter.lastMatch) {
		reporter.lastMatch = found
	}
}

// finish joins the reporter after backends have drained and joined their savers.
// Repeated calls cannot print another summary or change the authoritative result.
func (reporter *presentation) finish(totals runTotals, runError error) error {
	reporter.finishOnce.Do(func() {
		reporter.monitor.Stop()
		<-reporter.done

		reporter.mutex.Lock()
		defer reporter.mutex.Unlock()

		elapsed := time.Since(reporter.started)

		if !reporter.configured {
			reporter.setup.gpu.state = totals.gpu.state
			reporter.printSetup()
		}

		if reporter.outputError != nil {
			runError = errors.Join(runError, reporter.outputError)
		}

		reason := "completed"

		if errors.Is(runError, context.Canceled) && reporter.outputError == nil {
			reason = "interrupted"
		} else if runError != nil {
			reason = "failed"
		}

		line := reporter.buffer[:0]

		if reason == "failed" {
			line = fmt.Appendf(line, "Error: %v\n", runError)
		}

		line = fmt.Appendf(line, "Stopped: %s | elapsed %s\n  Overall averages use the same full-run wall clock.\n", reason, elapsed.Round(time.Millisecond))
		line = appendBackendSummary(line, "CPU", totals.cpu, elapsed)
		line = appendBackendSummary(line, "GPU", totals.gpu, elapsed)
		line = appendBackendSummary(line, "Total", backendTotals{stats: totals.combined()}, elapsed)
		line = append(line, "  Saved  "...)
		line = appendExact(line, totals.combined().Saved)
		line = fmt.Appendf(line, " matches\n  Output %s\n", reporter.setup.output)

		reporter.write(reporter.stderr, line)

		if reason == "failed" {
			reporter.resultError = reportedError{err: runError}
		}

		// If output itself failed, main must still attempt to report that failure.
		if reporter.outputError != nil {
			reporter.resultError = errors.Join(runError, reporter.outputError)
		}
	})

	return reporter.resultError
}

func (reporter *presentation) printSetup() {
	if reporter.configured {
		return
	}

	reporter.configured = true
	setup := &reporter.setup

	line := fmt.Appendf(reporter.buffer[:0], "Search setup\n  Patterns    %d", len(setup.input.texts))

	if setup.input.file != "" {
		line = fmt.Appendf(line, " from %s\n", setup.input.file)
	} else if len(setup.input.texts) == 1 && len(setup.input.texts[0]) <= 24 {
		line = fmt.Appendf(line, " from command line (%s)\n", setup.input.texts[0])
	} else {
		line = append(line, " from command line\n"...)
	}

	if setup.workers == 0 {
		line = append(line, "  CPU         disabled\n"...)
	} else {
		line = fmt.Appendf(line, "  CPU         %d worker(s), %s\n", setup.workers, setup.placement)
	}

	line = fmt.Appendf(line, "  Features    %s\n", setup.config.Features)
	line = fmt.Appendf(line, "  Build       %s\n", search.BuildDescription())

	if setup.workers == 0 {
		line = fmt.Appendf(line, "  Engine      inactive (--simd=%s)\n", setup.config.Requested)
	} else {
		selection := "auto"

		if setup.config.Forced() {
			selection = "forced"
		}

		line = fmt.Appendf(line, "  Engine      %s, %s; %s\n", setup.config.Arithmetic(), selection, setup.config.Generator())
		line = fmt.Appendf(line, "  Matching    %s\n", setup.config.Matching)

		if setup.config.NeedsChecksum {
			checksum := "Go SHA3"

			if setup.config.Checksum {
				checksum = "AVX-512F Keccak"
			}

			line = fmt.Appendf(line, "  Checksum    %s\n", checksum)
		}

		if setup.config.Forced() && !setup.config.Supported() {
			line = append(line, "  Warning     forced backend not reported usable; execution may fault.\n"...)
		}
	}

	line = append(line, "  GPU         "...)

	if setup.gpu.device != "" {
		line = fmt.Appendf(line, "%s\n", setup.gpu.device)
	} else {
		line = fmt.Appendf(line, "%s\n", setup.gpu.state)
	}

	if setup.gpu.selection > 0 {
		line = fmt.Appendf(line, "              %d streams (%s), %d rounds (%s)\n", setup.gpu.streams, settingSource(setup.gpu.autoStreams), setup.gpu.rounds, settingSource(setup.gpu.autoRounds))
	} else if setup.gpu.device != "" {
		line = append(line, "              configuration not selected\n"...)
	}

	line = fmt.Appendf(line, "  Startup     input + host setup %.2fs\n", setup.prepared.Seconds())

	if setup.gpu.device != "" {
		line = fmt.Appendf(line, "              GPU initialization %.2fs\n", setup.gpu.initialization.Seconds())
	}

	if setup.gpu.first > 0 {
		line = fmt.Appendf(line, "              first GPU submission at %.2fs since start\n", setup.gpu.first.Seconds())
	}

	if setup.gpu.selection > 0 {
		line = fmt.Appendf(line, "              configuration selected at %.2fs since start\n", setup.gpu.selection.Seconds())
	}

	line = fmt.Appendf(line, "  Output      %s\n  Stop        Ctrl+C (drains accepted matches)\n", setup.output)
	line = appendCandidateEstimate(line, reporter.estimate)

	reporter.write(reporter.stderr, line)
}

// Call with the presentation lock held, or before its goroutine starts.
func (reporter *presentation) write(writer io.Writer, data []byte) {
	written, err := writer.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}

	if err != nil && reporter.outputError == nil {
		reporter.outputError = fmt.Errorf("write terminal output: %w", err)
		reporter.cancel()
	}
}

func newPresentation(stdout, stderr io.Writer, started time.Time, cancel context.CancelFunc) *presentation {
	return &presentation{stdout: stdout, stderr: stderr, started: started, lastMatch: started, cancel: cancel}
}

func appendBackendSummary(buffer []byte, name string, backend backendTotals, elapsed time.Duration) []byte {
	buffer = fmt.Appendf(buffer, "  %-5s  ", name)
	buffer = appendExact(buffer, backend.stats.Checked)
	buffer = append(buffer, " checked | "...)
	buffer = appendCompact(buffer, checkedRate(backend.stats.Checked, elapsed))
	buffer = append(buffer, " keys/s avg"...)

	if backend.state != "" {
		buffer = fmt.Appendf(buffer, " | %s", backend.state)
	}

	if backend.device != "" {
		buffer = fmt.Appendf(buffer, " (%s)", backend.device)
	}

	return append(buffer, '\n')
}

func settingSource(automatic bool) string {
	if automatic {
		return "auto"
	}

	return "explicit"
}
