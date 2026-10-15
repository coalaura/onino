package search

import "sync"

// Monitor exposes fresh cumulative snapshots without scheduling reports. Readers
// must only load published counters and remain valid after their backend returns.
// A nil monitor disables observation; Stop ends reporting, not backend draining.
type Monitor struct {
	mutex   sync.Mutex
	readers []monitorReader
	done    chan struct{}
	stop    sync.Once
}

type monitorReader struct {
	snapshot func() Stats
	gpu      bool
}

func (monitor *Monitor) Observe(snapshot func() Stats) {
	monitor.observe(snapshot, false)
}

func (monitor *Monitor) ObserveGPU(snapshot func() Stats) {
	monitor.observe(snapshot, true)
}

// Backends loads published counters without waiting for in-flight work. The two
// backends publish asynchronously; callers should bracket the read with a clock.
func (monitor *Monitor) Backends() (Stats, Stats) {
	monitor.mutex.Lock()
	defer monitor.mutex.Unlock()

	var (
		cpu Stats
		gpu Stats
	)

	for _, reader := range monitor.readers {
		stats := reader.snapshot()

		if reader.gpu {
			gpu.Checked += stats.Checked
			gpu.Saved += stats.Saved
		} else {
			cpu.Checked += stats.Checked
			cpu.Saved += stats.Saved
		}
	}

	return cpu, gpu
}

func (monitor *Monitor) observe(snapshot func() Stats, gpu bool) {
	if monitor == nil {
		return
	}

	monitor.mutex.Lock()
	monitor.readers = append(monitor.readers, monitorReader{snapshot: snapshot, gpu: gpu})
	monitor.mutex.Unlock()
}

func (monitor *Monitor) Snapshot() Stats {
	cpu, gpu := monitor.Backends()

	return Stats{Checked: cpu.Checked + gpu.Checked, Saved: cpu.Saved + gpu.Saved}
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
	return &Monitor{readers: make([]monitorReader, 0, 2), done: make(chan struct{})}
}
