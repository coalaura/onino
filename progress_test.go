package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coalaura/onino/internal/search"
)

type rateCase struct {
	name     string
	previous uint64
	checked  uint64
	interval time.Duration
	want     float64
}

type reportingMode struct {
	name  string
	cpu   bool
	gpu   bool
	saved uint64
	want  string
}

type delayedProgressWriter struct {
	bytes.Buffer
	release chan struct{}
	blocked bool
}

func (writer *delayedProgressWriter) Write(data []byte) (int, error) {
	if !writer.blocked {
		writer.blocked = true
		<-writer.release
	}

	return writer.Buffer.Write(data)
}

func TestIntervalRate(t *testing.T) {
	started := time.Now()

	cases := []rateCase{
		{name: "initial baseline", checked: 200, interval: 4 * time.Second, want: 50},
		{name: "startup excluded later", previous: 200, checked: 1000, interval: 4 * time.Second, want: 200},
		{name: "actual delayed interval", previous: 1000, checked: 1900, interval: 4500 * time.Millisecond, want: 200},
		{name: "no completed work", previous: 1900, checked: 1900, interval: 4 * time.Second},
		{name: "zero duration", previous: 1900, checked: 2000},
		{name: "negative duration", checked: 100, interval: -time.Second},
		{name: "counter regression", previous: 2000, checked: 1900, interval: time.Second},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			previous := progressSample{checked: test.previous, time: started}
			current := progressSample{checked: test.checked, time: started.Add(test.interval)}

			actual := current.rateSince(previous)

			if actual != test.want {
				t.Fatalf("rate = %v, want %v", actual, test.want)
			}
		})
	}
}

func TestUnifiedProgress(t *testing.T) {
	modes := []reportingMode{
		{name: "CPU only", cpu: true, saved: 1, want: "5s | 500 checked | 100/s"},
		{name: "GPU only", gpu: true, saved: 2, want: "5s | 1k checked | 200/s"},
		{name: "mixed", cpu: true, gpu: true, saved: 3, want: "5s | 1.5k checked | 300/s"},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var (
					output     bytes.Buffer
					cpuChecked atomic.Uint64
					gpuChecked atomic.Uint64
				)

				monitor := search.NewMonitor()
				defer monitor.Stop()

				reporter := startTestProgress(context.Background(), &output, matchEstimate{candidates50: 600, candidates95: 1200}, monitor)

				// Registration happens after the explicit zero/time startup baseline.
				if mode.cpu {
					monitor.Observe(func() search.Stats {
						return search.Stats{Checked: cpuChecked.Load(), Saved: 1}
					})
				}

				if mode.gpu {
					monitor.Observe(func() search.Stats {
						return search.Stats{Checked: gpuChecked.Load(), Saved: 2}
					})
				}

				synctest.Wait()
				time.Sleep(250 * time.Millisecond)

				// Independent publication schedules cannot trigger output or cache a
				// CPU snapshot at a GPU publication boundary.
				for range 5 {
					gpuChecked.Add(200)
					time.Sleep(500 * time.Millisecond)

					cpuChecked.Add(100)
					time.Sleep(500 * time.Millisecond)
				}

				time.Sleep(5 * time.Second)
				synctest.Wait()

				monitor.Stop()
				<-reporter.done

				if strings.Count(output.String(), " | wait ") != 2 || !strings.Contains(output.String(), mode.want) {
					t.Fatalf("did not sample fresh combined counters: %s", output.String())
				}

				if monitor.Snapshot().Saved != mode.saved {
					t.Fatal("snapshot lost combined saved counts")
				}

				if mode.cpu && mode.gpu && !strings.Contains(output.String(), "50% ~2s / 95% ~4s") {
					t.Fatalf("wait estimates did not use combined recent rate: %s", output.String())
				}

				if !strings.Contains(output.String(), "10s | ") || !strings.Contains(output.String(), " | 0/s | saved ") || !strings.Contains(output.String(), "50% stalled / 95% stalled") {
					t.Fatalf("idle interval retained old throughput or estimates: %s", output.String())
				}
			})
		})
	}
}

func TestDelayedProgressSampling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var checked atomic.Uint64

		monitor := search.NewMonitor()
		defer monitor.Stop()

		monitor.Observe(func() search.Stats {
			return search.Stats{Checked: checked.Load()}
		})

		output := &delayedProgressWriter{release: make(chan struct{})}

		reporter := startTestProgress(context.Background(), output, matchEstimate{}, monitor)

		synctest.Wait()

		checked.Store(50)

		time.Sleep(5 * time.Second)

		synctest.Wait()

		// The second tick carries time 10s but cannot be sampled until time 11s.
		time.Sleep(6 * time.Second)

		checked.Store(110)

		close(output.release)

		synctest.Wait()

		if strings.Count(output.String(), " | 10/s | ") != 2 || !strings.Contains(output.String(), "11s | 110 checked") {
			t.Fatalf("used scheduled rather than actual sample time: %s", output.String())
		}

		monitor.Stop()
		<-reporter.done
	})
}

func TestProgressShutdown(t *testing.T) {
	causes := []string{"cancellation", "backend completion"}

	for _, cause := range causes {
		t.Run(cause, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				monitor := search.NewMonitor()
				defer monitor.Stop()

				output := &delayedProgressWriter{release: make(chan struct{})}
				reporter := startTestProgress(ctx, output, matchEstimate{}, monitor)

				synctest.Wait()

				time.Sleep(11 * time.Second)

				synctest.Wait()

				if cause == "cancellation" {
					cancel()
				} else {
					monitor.Stop()
				}

				close(output.release)

				synctest.Wait()

				<-reporter.done

				// Draining changes final totals after periodic reporting has stopped.
				time.Sleep(4 * time.Second)

				monitor.Stop()

				totals := runTotals{cpu: backendTotals{stats: search.Stats{Checked: 1500, Saved: 7}, state: backendEnabled}, gpu: backendTotals{state: backendDisabled}}

				reporter.finish(totals, ctx.Err())
				reporter.finish(runTotals{}, nil)

				time.Sleep(8 * time.Second)

				synctest.Wait()

				if strings.Count(output.String(), " | wait ") != 1 || strings.Count(output.String(), "Stopped:") != 1 {
					t.Fatalf("duplicate shutdown reporting: %s", output.String())
				}

				if !strings.Contains(output.String(), "Total  1,500 checked | 100 keys/s avg") || !strings.Contains(output.String(), "elapsed 15s") || !strings.Contains(output.String(), "Saved  7 matches") {
					t.Fatalf("final report lost drained totals or overall runtime: %s", output.String())
				}
			})
		})
	}
}

func TestProgressStoppedWhileWaitingForOutput(t *testing.T) {
	causes := []string{"cancellation", "backend completion"}

	for _, cause := range causes {
		t.Run(cause, func(t *testing.T) {
			var output bytes.Buffer

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			monitor := search.NewMonitor()
			defer monitor.Stop()

			reporter := newPresentation(&output, &output, time.Now(), cancel)
			reporter.monitor = monitor

			reporter.mutex.Lock()

			pending := make(chan struct{})
			done := make(chan struct{})

			go func() {
				close(pending)
				reporter.sample(ctx)
				close(done)
			}()

			<-pending

			if cause == "cancellation" {
				cancel()
			} else {
				monitor.Stop()
			}

			reporter.mutex.Unlock()

			<-done

			if output.Len() != 0 {
				t.Fatalf("printed a pending report after stopping: %s", output.String())
			}
		})
	}
}

func startTestProgress(ctx context.Context, output io.Writer, estimate matchEstimate, monitor *search.Monitor) *presentation {
	reporter := newPresentation(output, output, time.Now(), func() {})

	reporter.configured = true
	reporter.start(ctx, searchSetup{}, estimate, monitor)

	return reporter
}
