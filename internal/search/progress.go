package search

import "time"

const progressInterval = 4 * time.Second
const progressCheckKeys = batchSize * 128

type progressReporter struct {
	report func(Stats)
	next   time.Time
	saved  uint64
}

func (reporter *progressReporter) update(stats Stats) {
	if reporter.report == nil {
		return
	}

	// Amortize clock reads across miss-only batches. Saving can be slow, so
	// batches with matches check the deadline immediately after completing.
	if stats.Checked%progressCheckKeys != 0 && stats.Saved == reporter.saved {
		return
	}

	reporter.saved = stats.Saved

	now := time.Now()

	if now.Before(reporter.next) {
		return
	}

	reporter.next = now.Add(progressInterval)

	reporter.report(stats)
}
