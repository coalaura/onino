//go:build measure

package search

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/cpu"
)

type measureProcess struct {
	command *exec.Cmd
	output  bytes.Buffer
}

// TestMeasureProcesses alternates one production runner with concurrent pinned
// single-worker reference processes. Children initialize/warm up before a common
// start time; every process exits after the requested bounded interval.
func TestMeasureProcesses(t *testing.T) {
	requested := os.Getenv("ONINO_PROCESSES")
	if requested == "" {
		t.Skip("set ONINO_PROCESSES=2,8,16,32")
	}

	topology, err := cpu.Discover()
	if err != nil {
		t.Fatal(err)
	}

	duration := time.Duration(measureInteger(t, "ONINO_SECONDS", 5)) * time.Second

	repeats := measureInteger(t, "ONINO_REPEATS", 3)

	for count := range strings.SplitSeq(requested, ",") {
		workers, resolveError := cpu.Resolve(count, len(topology.CPUs))
		if resolveError != nil {
			t.Fatal(resolveError)
		}

		processors, selectError := cpu.Select(topology, workers, true)
		if selectError != nil {
			t.Fatal(selectError)
		}

		previous := runtime.GOMAXPROCS(workers)

		for repeat := range repeats {
			for phase := range 2 {
				var result measurement

				if (repeat+phase)%2 == 0 {
					config := measureConfig{workload: "rare", placement: "spread", mode: "parallel", workers: workers, duration: duration, processors: processors}
					result = measureSearch(t, config)
				} else {
					result = measureProcesses(t, processors, duration)
				}

				result.Repeat = repeat

				encoded, encodeError := json.Marshal(result)
				if encodeError != nil {
					t.Fatal(encodeError)
				}

				fmt.Printf("MEASURE %s\n", encoded)
			}
		}

		runtime.GOMAXPROCS(previous)
	}
}

func measureProcesses(t *testing.T, processors []cpu.CPU, duration time.Duration) measurement {
	t.Helper()

	environment := make([]string, 0, len(os.Environ())+10)

	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "ONINO_") && !strings.HasPrefix(value, "GOMAXPROCS=") {
			environment = append(environment, value)
		}
	}

	start := time.Now().Add(4 * time.Second).UnixNano()
	environment = append(environment, "GOMAXPROCS=1", "ONINO_MEASURE=rare", "ONINO_WORKERS=1", "ONINO_PLACEMENTS=spread", "ONINO_MODES=reference", "ONINO_REPEATS=1")
	environment = append(environment, "ONINO_SECONDS="+strconv.Itoa(int(duration/time.Second)), "ONINO_PROCESS_START="+strconv.FormatInt(start, 10))
	processes := make([]measureProcess, len(processors))

	for index, processor := range processors {
		encoded, err := json.Marshal(processor)
		if err != nil {
			t.Fatal(err)
		}

		process := &processes[index]

		process.command = exec.Command(os.Args[0], "-test.run=^TestMeasureMulticore$", "-test.v", "-test.timeout=2m")
		process.command.Env = append(slices.Clip(environment), "ONINO_PROCESS_CPU="+string(encoded))
		process.command.Stdout = &process.output
		process.command.Stderr = &process.output

		err = process.command.Start()
		if err != nil {
			for started := range index {
				processes[started].command.Process.Kill()
				processes[started].command.Wait()
			}

			t.Fatal(err)
		}
	}

	total := measurement{Workload: "rare", Placement: "processes-spread", Mode: "reference", Workers: len(processors), CPUs: processors}

	var failed bool

	for index := range processes {
		process := &processes[index]

		err := process.command.Wait()
		if err != nil {
			t.Errorf("child: %v\n%s", err, &process.output)
			failed = true

			continue
		}

		for line := range strings.SplitSeq(process.output.String(), "\n") {
			if !strings.HasPrefix(line, "MEASURE ") {
				continue
			}

			var result measurement

			err = json.Unmarshal([]byte(strings.TrimPrefix(line, "MEASURE ")), &result)
			if err != nil {
				t.Error(err)
				failed = true
			}

			total.Seconds = max(total.Seconds, result.Seconds)
			total.Checked += result.Checked
			total.Saved += result.Saved
			total.Allocations += result.Allocations
			total.Bytes += result.Bytes
		}
	}

	if failed {
		t.FailNow()
	}

	total.KeysPerSecond = float64(total.Checked) / total.Seconds
	total.PerWorker = total.KeysPerSecond / float64(total.Workers)

	return total
}
