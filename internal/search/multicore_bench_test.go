//go:build measure

package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

type measurement struct {
	Workload      string
	Placement     string
	Mode          string
	Workers       int
	Repeat        int
	Seconds       float64
	KeysPerSecond float64
	PerWorker     float64
	Checked       uint64
	Saved         uint64
	Allocations   uint64
	Bytes         uint64
	CPUs          []cpu.CPU
}

type measureConfig struct {
	workload   string
	placement  string
	mode       string
	workers    int
	duration   time.Duration
	processors []cpu.CPU
}

// TestMeasureMulticore is an opt-in bounded experiment, not part of the ordinary
// test suite. A single startup gate excludes initialization and a 200ms warm-up
// per worker. Production uses the actual coordinator/worker loop; reference has
// only independent batch loops and local counters. No recurring gates or queues.
func TestMeasureMulticore(t *testing.T) {
	requested := os.Getenv("ONINO_MEASURE")
	if requested == "" {
		t.Skip("set ONINO_MEASURE=rare,frequent,512,shared512,all_hits,persistence")
	}

	topology, err := cpu.Discover()
	if err != nil {
		t.Fatal(err)
	}

	seconds := measureInteger(t, "ONINO_SECONDS", 5)
	repeats := measureInteger(t, "ONINO_REPEATS", 3)

	workers := measureList("ONINO_WORKERS", "1,2,4,8,16,24,32")
	placements := measureList("ONINO_PLACEMENTS", "os,packed,spread")
	modes := measureList("ONINO_MODES", "reference,parallel,progress")

	workloads := strings.SplitSeq(requested, ",")

	for workload := range workloads {
		for _, count := range workers {
			workerCount, parseError := strconv.Atoi(count)
			if parseError != nil || workerCount < 1 {
				t.Fatalf("invalid worker count %q", count)
			}

			if workerCount > len(topology.CPUs) {
				continue
			}

			previous := runtime.GOMAXPROCS(workerCount)

			for repeat := range repeats {
				for placementIndex := range placements {
					if repeat%2 != 0 {
						placementIndex = len(placements) - placementIndex - 1
					}

					placement := placements[placementIndex]

					var processors []cpu.CPU

					if placement != "os" {
						if placement != "packed" && placement != "spread" {
							t.Fatalf("unknown placement %q", placement)
						}

						processors, err = cpu.Select(topology, workerCount, placement == "spread")
						if err != nil {
							t.Fatal(err)
						}
					}

					for index := range modes {
						modeIndex := index

						if repeat%2 != 0 {
							modeIndex = len(modes) - index - 1
						}

						config := measureConfig{
							workload:   workload,
							placement:  placement,
							mode:       modes[modeIndex],
							workers:    workerCount,
							duration:   time.Duration(seconds) * time.Second,
							processors: processors,
						}

						processCPU := os.Getenv("ONINO_PROCESS_CPU")
						if processCPU != "" {
							var processor cpu.CPU

							err = json.Unmarshal([]byte(processCPU), &processor)
							if err != nil || workerCount != 1 {
								t.Fatalf("invalid child CPU: %s %v", processCPU, err)
							}

							config.processors = []cpu.CPU{processor}
						}

						result := measureSearch(t, config)
						result.Repeat = repeat

						encoded, encodeError := json.Marshal(result)
						if encodeError != nil {
							t.Fatal(encodeError)
						}

						fmt.Printf("MEASURE %s\n", encoded)
					}
				}
			}

			runtime.GOMAXPROCS(previous)
		}
	}
}

func measureSearch(t *testing.T, config measureConfig) measurement {
	t.Helper()

	patterns := []string{"somethingrare."}

	switch config.workload {
	case "rare":
	case "frequent":
		patterns = []string{"ab."}
	case "512":
		patterns = benchmarkDictionary(512, false)
	case "shared512":
		patterns = benchmarkDictionary(512, true)
	case "all_hits", "persistence":
		patterns = allSuffixPatterns(1)
	default:
		found := false

		for _, test := range anchoredBenchmarkCases() {
			if test.name == config.workload {
				patterns = test.patterns
				found = true

				break
			}
		}

		if !found {
			t.Fatalf("unknown workload %q", config.workload)
		}
	}

	matcher := parallelMatcher(t, patterns...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{}, config.workers)
	start := make(chan struct{})
	done := make(chan searchResult, 1)

	save := SaveFunc(cheapSave)

	if config.workload == "persistence" {
		if config.mode == "reference" {
			t.Fatal("persistence requires serialized production callbacks")
		}

		store, err := onion.NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		save = store.Save
	}

	hooks := testParallelHooks()

	var synchronizedStart time.Time

	startValue := os.Getenv("ONINO_PROCESS_START")
	if startValue != "" {
		startNanos, parseError := strconv.ParseInt(startValue, 10, 64)
		if parseError != nil {
			t.Fatal(parseError)
		}

		synchronizedStart = time.Unix(0, startNanos)
	}

	hooks.create = func(index int, matcher *pattern.Matcher) (*worker, error) {
		state, err := deterministicWorker(index, matcher)

		if config.workload == "persistence" {
			state, err = createSecureWorker(index, matcher)
		}

		var warm Stats

		if err == nil {
			until := time.Now().Add(200 * time.Millisecond)

			for time.Now().Before(until) {
				err = state.searchBatch(matcher, cheapSave, &warm)
				if err != nil {
					break
				}
			}
		}

		ready <- struct{}{}
		<-start

		return state, err
	}

	options := Options{Workers: config.workers, CPUs: config.processors}

	if config.mode == "progress" {
		options.Progress = func(Stats) {}
	} else if config.mode != "parallel" && config.mode != "reference" {
		t.Fatalf("unknown mode %q", config.mode)
	}

	go func() {
		var (
			stats Stats
			err   error
		)

		if config.mode == "reference" {
			stats, err = independentSearch(ctx, matcher, options, hooks)
		} else {
			stats, err = runParallel(ctx, matcher, save, options, hooks)
		}

		done <- searchResult{stats: stats, err: err}
	}()

	for range config.workers {
		select {
		case <-ready:
		case result := <-done:
			close(start)

			t.Fatalf("measurement startup: %v", result.err)
		case <-time.After(30 * time.Second):
			close(start)
			cancel()

			<-done

			t.Fatal("measurement startup timed out")
		}
	}

	var (
		before runtime.MemStats
		after  runtime.MemStats
	)

	if !synchronizedStart.IsZero() {
		if time.Now().After(synchronizedStart) {
			close(start)
			cancel()
			<-done
			t.Fatal("child missed synchronized start")
		}

		time.Sleep(time.Until(synchronizedStart))
	}

	runtime.ReadMemStats(&before)

	started := time.Now()
	timer := time.AfterFunc(config.duration, cancel)

	close(start)

	result := <-done

	elapsed := time.Since(started).Seconds()
	timer.Stop()

	runtime.ReadMemStats(&after)

	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("measurement failed: %v", result.err)
	}

	rate := float64(result.stats.Checked) / elapsed

	return measurement{
		Workload:      config.workload,
		Placement:     config.placement,
		Mode:          config.mode,
		Workers:       config.workers,
		Seconds:       elapsed,
		KeysPerSecond: rate,
		PerWorker:     rate / float64(config.workers),
		Checked:       result.stats.Checked,
		Saved:         result.stats.Saved,
		Allocations:   after.Mallocs - before.Mallocs,
		Bytes:         after.TotalAlloc - before.TotalAlloc,
		CPUs:          config.processors,
	}
}

func independentSearch(ctx context.Context, matcher *pattern.Matcher, options Options, hooks parallelHooks) (Stats, error) {
	done := make(chan searchResult, options.Workers)

	for index := range options.Workers {
		go independentWorker(ctx, matcher, options, hooks, index, done)
	}

	var (
		total   Stats
		failure error
	)

	for range options.Workers {
		result := <-done

		total.Checked += result.stats.Checked
		total.Saved += result.stats.Saved

		failure = errors.Join(failure, result.err)
	}

	if failure != nil {
		return total, failure
	}

	return total, ctx.Err()
}

func independentWorker(ctx context.Context, matcher *pattern.Matcher, options Options, hooks parallelHooks, index int, done chan<- searchResult) {
	var result searchResult

	defer func() {
		done <- result
	}()

	if len(options.CPUs) != 0 {
		restore, err := hooks.pin(options.CPUs[index])
		if err != nil {
			result.err = err

			return
		}

		defer func() {
			result.err = errors.Join(result.err, restore())
		}()
	}

	state, err := hooks.create(index, matcher)
	if err != nil {
		result.err = err

		return
	}

	for ctx.Err() == nil {
		err = state.searchBatch(matcher, cheapSave, &result.stats)
		if err != nil {
			result.err = err

			return
		}
	}
}

func measureInteger(t *testing.T, name string, fallback int) int {
	t.Helper()

	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 3600 {
		t.Fatalf("invalid %s=%q", name, value)
	}

	return parsed
}

func measureList(name string, fallback string) []string {
	value := os.Getenv(name)
	if value == "" {
		value = fallback
	}

	return strings.Split(value, ",")
}
