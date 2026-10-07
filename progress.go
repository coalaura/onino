package main

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/coalaura/onino/internal/search"
)

const reportInterval = 4 * time.Second

type progressSample struct {
	checked uint64
	time    time.Time
}

type progressCoordinator struct {
	monitor  *search.Monitor
	output   io.Writer
	estimate matchEstimate
	started  time.Time
	previous progressSample
	done     chan struct{}
	buffer   [320]byte
}

// Both CLI streams share this lock because callers may give them the same writer.
type serializedWriter struct {
	writer io.Writer
	mutex  *sync.Mutex
}

func (sample progressSample) rateSince(previous progressSample) float64 {
	if sample.checked < previous.checked {
		return 0
	}

	return checkedRate(sample.checked-previous.checked, sample.time.Sub(previous.time))
}

func (reporter *progressCoordinator) run(ctx context.Context) {
	defer close(reporter.done)

	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-reporter.monitor.Done():
			return
		case <-ticker.C:
			// Cancellation takes precedence even when a tick is already pending.
			if ctx.Err() != nil {
				return
			}

			select {
			case <-reporter.monitor.Done():
				return
			default:
			}

			stats := reporter.monitor.Snapshot()

			// Use the sampling time, not the ticker's possibly delayed timestamp.
			sample := progressSample{checked: stats.Checked, time: time.Now()}
			rate := sample.rateSince(reporter.previous)

			reporter.previous = sample

			elapsed := sample.time.Sub(reporter.started).Truncate(time.Second)

			line := appendSearchStatus(reporter.buffer[:0], stats, elapsed, rate, reporter.estimate, false)
			reporter.output.Write(line)
		}
	}
}

// finish joins periodic output before using the authoritative drained totals.
func (reporter *progressCoordinator) finish(stats search.Stats) {
	reporter.monitor.Stop()
	<-reporter.done

	elapsed := time.Since(reporter.started)
	rate := checkedRate(stats.Checked, elapsed)

	line := appendSearchStatus(reporter.buffer[:0], stats, elapsed.Round(time.Millisecond), rate, reporter.estimate, true)
	reporter.output.Write(line)
}

func (writer serializedWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()

	return writer.writer.Write(data)
}

func startProgress(ctx context.Context, output io.Writer, estimate matchEstimate, monitor *search.Monitor, started time.Time) *progressCoordinator {
	reporter := &progressCoordinator{
		monitor:  monitor,
		output:   output,
		estimate: estimate,
		started:  started,
		previous: progressSample{checked: monitor.Snapshot().Checked, time: started},
		done:     make(chan struct{}),
	}

	go reporter.run(ctx)

	return reporter
}

func checkedRate(checked uint64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}

	return float64(checked) / elapsed.Seconds()
}
