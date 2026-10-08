package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/search"
	"github.com/coalaura/onino/internal/simd"
)

type lifecycleCase struct {
	name   string
	totals runTotals
	err    error
	reason string
}

type failingOutput struct {
	err error
}

func (writer failingOutput) Write(data []byte) (int, error) {
	return 0, writer.err
}

func TestFinalLifecycleAndBackendIdentity(t *testing.T) {
	cpu := backendTotals{stats: search.Stats{Checked: 1000, Saved: 1}, state: backendEnabled}
	gpu := backendTotals{stats: search.Stats{Checked: 2000, Saved: 2}, state: backendEnabled, device: "Test GPU"}

	failure := errors.New("GPU initialization failed")

	cases := []lifecycleCase{
		{name: "CPU only completed", totals: runTotals{cpu: cpu, gpu: backendTotals{state: backendDisabled}}, reason: "completed"},
		{name: "GPU only interrupted", totals: runTotals{cpu: backendTotals{state: backendDisabled}, gpu: gpu}, err: context.Canceled, reason: "interrupted"},
		{name: "mixed", totals: runTotals{cpu: cpu, gpu: gpu}, reason: "completed"},
		{name: "partially initialized", totals: runTotals{cpu: cpu, gpu: backendTotals{state: backendUnavailable}}, err: failure, reason: "failed"},
		{name: "enabled zero work", totals: runTotals{cpu: backendTotals{state: backendEnabled}, gpu: backendTotals{state: backendEnabled, device: "Test GPU"}}, err: context.Canceled, reason: "interrupted"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var output bytes.Buffer

				monitor := search.NewMonitor()

				reporter := newPresentation(&output, &output, time.Now(), func() {})

				setup := searchSetup{input: patternInput{texts: []string{"rare."}}, output: "results with spaces", gpu: gpuSetup{state: backendDisabled}}

				reporter.start(context.Background(), setup, newMatchEstimate(1.0/32), monitor)

				time.Sleep(6 * time.Second)

				monitor.Stop()

				<-reporter.done

				// Accepted work and saves can complete after periodic reporting ends.
				time.Sleep(4 * time.Second)

				var key onion.Key

				reporter.saved(key, time.Now())

				err := reporter.finish(test.totals, test.err)
				if test.reason == "failed" && !errors.Is(err, failure) {
					t.Fatalf("lost error: %v", err)
				}

				if test.reason != "failed" && err != nil {
					t.Fatal(err)
				}

				text := output.String()

				reporter.finish(runTotals{}, errors.New("late failure"))

				time.Sleep(10 * time.Second)

				synctest.Wait()

				if output.String() != text || strings.Count(text, "Stopped:") != 1 || !strings.Contains(text, "Stopped: "+test.reason+" | elapsed 10s") {
					t.Fatalf("final lifecycle: %s", output.String())
				}

				if strings.Index(text, ".onion saved") > strings.Index(text, "Stopped:") || !strings.HasSuffix(text, "  Output results with spaces\n") {
					t.Fatalf("final ordering: %s", text)
				}

				backends := []backendTotals{test.totals.cpu, test.totals.gpu, {stats: test.totals.combined()}}
				names := []string{"CPU", "GPU", "Total"}

				for index, backend := range backends {
					want := string(appendBackendSummary(nil, names[index], backend, 10*time.Second))
					if !strings.Contains(text, want) {
						t.Fatalf("lost identity or shared denominator: want %s in %s", want, text)
					}
				}
			})
		})
	}
}

func TestForcedSetupDetection(t *testing.T) {
	var output bytes.Buffer

	reporter := newPresentation(&output, &output, time.Now(), func() {})

	reporter.setup = searchSetup{
		input:     patternInput{texts: []string{"rare."}},
		workers:   1,
		placement: "OS placement",
		config: search.Configuration{
			Requested: simd.BMI2ADX,
			Engine:    simd.BMI2ADX,
			Scalar:    simd.BMI2ADX,
			Features:  simd.Features{Known: true, AVX2: true, BMI2: true},
			Matching:  "scalar prefix",
		},
		gpu: gpuSetup{state: backendDisabled},
	}

	reporter.printSetup()
	reporter.printSetup()

	text := output.String()

	wants := []string{"AVX2, BMI2; ADX not reported", "scalar (bmi2+adx), forced; paired", "scalar prefix", "forced backend not reported usable; execution may fault"}

	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}

	if strings.Count(text, "Search setup") != 1 || strings.Count(text, "Warning") != 1 || strings.Contains(text, "Checksum") {
		t.Fatalf("duplicate or fictitious execution reporting: %s", text)
	}
}

func TestExactFinalTotalsAndSharedAverages(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer

		monitor := search.NewMonitor()

		reporter := startTestProgress(context.Background(), &output, matchEstimate{}, monitor)

		time.Sleep(10 * time.Second)

		totals := runTotals{cpu: backendTotals{stats: search.Stats{Checked: 1234, Saved: 2}}, gpu: backendTotals{stats: search.Stats{Checked: 2345, Saved: 3}}}

		reporter.finish(totals, nil)

		text := output.String()

		wants := []string{"CPU    1,234 checked | 123 keys/s avg", "GPU    2,345 checked | 235 keys/s avg", "Total  3,579 checked | 358 keys/s avg", "Saved  5 matches"}

		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q: %s", want, text)
			}
		}

		totals.cpu.stats.Checked = math.MaxUint64 - 3
		totals.gpu.stats.Checked = 3

		line := string(appendBackendSummary(nil, "Total", backendTotals{stats: totals.combined()}, time.Second))
		if !strings.Contains(line, "18,446,744,073,709,551,615 checked") {
			t.Fatalf("rounded integer total: %s", line)
		}
	})
}

func TestOutputFailureCancelsAndPropagates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		failure := errors.New("broken output")

		monitor := search.NewMonitor()

		reporter := newPresentation(io.Discard, failingOutput{err: failure}, time.Now(), cancel)
		reporter.start(ctx, searchSetup{}, matchEstimate{}, monitor)

		if ctx.Err() != context.Canceled {
			t.Fatal("output failure did not stop the search")
		}

		err := reporter.finish(runTotals{}, ctx.Err())
		if !errors.Is(err, failure) {
			t.Fatalf("output failure lost: %v", err)
		}
	})
}

func TestSetupDoesNotDumpPatterns(t *testing.T) {
	var output bytes.Buffer

	texts := make([]string, 500)

	for index := range texts {
		texts[index] = "somethingrare."
	}

	reporter := newPresentation(&output, &output, time.Now(), func() {})

	reporter.setup = searchSetup{input: patternInput{texts: texts, file: "many patterns.txt"}, output: "results", gpu: gpuSetup{state: backendDisabled}}
	reporter.printSetup()

	text := output.String()

	if !strings.Contains(text, "500 from many patterns.txt") || strings.Contains(text, "somethingrare.") || len(text) > 700 {
		t.Fatalf("verbose setup: %s", text)
	}
}
