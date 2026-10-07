package search

import "sync"

// Monitor exposes fresh cumulative snapshots without scheduling reports. Readers
// must only load published counters and remain valid after their backend returns.
// A nil monitor disables observation; Stop ends reporting, not backend draining.
type Monitor struct {
	mutex   sync.Mutex
	readers []func() Stats
	done    chan struct{}
	stop    sync.Once
}

func (monitor *Monitor) Observe(snapshot func() Stats) {
	if monitor == nil {
		return
	}

	monitor.mutex.Lock()
	monitor.readers = append(monitor.readers, snapshot)
	monitor.mutex.Unlock()
}

func (monitor *Monitor) Snapshot() Stats {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	var total Stats

	for _, snapshot := range monitor.readers {
		stats := snapshot()
		total.Checked += stats.Checked
		total.Saved += stats.Saved
	}

	return total
}

func (monitor *Monitor) Done() <-chan struct{} {
	return monitor.done
}

func (monitor *Monitor) Stop() {
	if monitor == nil {
		return
	}

	monitor.stop.Do(func() {
		close(monitor.done)
	})
}

func NewMonitor() *Monitor {
	return &Monitor{readers: make([]func() Stats, 0, 2), done: make(chan struct{})}
}
