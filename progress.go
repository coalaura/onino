package main

import (
	"context"
	"time"
)

const reportInterval = 5 * time.Second

type progressSample struct {
	checked uint64
	time    time.Time
}

func (sample progressSample) rateSince(previous progressSample) float64 {
	if sample.checked < previous.checked {
		return 0
	}

	return checkedRate(sample.checked-previous.checked, sample.time.Sub(previous.time))
}

func (reporter *presentation) run(ctx context.Context) {
	defer close(reporter.done)

	// Anchor the first report to the full-run clock, including input preparation.
	delay := max(0, time.Until(reporter.started.Add(reportInterval)))
	timer := time.NewTimer(delay)
	defer timer.Stop()

	var ticker *time.Ticker

	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	ticks := timer.C

	for {
		select {
		case <-ctx.Done():
			return
		case <-reporter.monitor.Done():
			return
		case <-ticks:
			if ctx.Err() != nil {
				return
			}

			select {
			case <-reporter.monitor.Done():
				return
			default:
			}

			if ticker == nil {
				ticker = time.NewTicker(reportInterval)
				ticks = ticker.C
			}

			reporter.sample(ctx)
		}
	}
}

func (reporter *presentation) sample(ctx context.Context) {
	reporter.mutex.Lock()
	defer reporter.mutex.Unlock()

	// A complete match or diagnostic block may have delayed this pending report.
	if ctx.Err() != nil {
		return
	}

	select {
	case <-reporter.monitor.Done():
		return
	default:
	}

	stats := reporter.monitor.Snapshot()

	// Sampling time includes scheduling and output delays, not a ticker timestamp.
	sample := progressSample{checked: stats.Checked, time: time.Now()}

	rate := sample.rateSince(reporter.previous)
	reporter.previous = sample

	line := appendSearchStatus(reporter.buffer[:0], stats, sample.time.Sub(reporter.started), rate, reporter.estimate, reporter.phase)

	reporter.write(reporter.stderr, line)
}

func checkedRate(checked uint64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}

	return float64(checked) / elapsed.Seconds()
}
