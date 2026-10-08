//go:build gpu

package main

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coalaura/onino/internal/search"
	"github.com/coalaura/onino/internal/simd"
)

func TestForcedSetupPrecedesGPUInitialization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var output bytes.Buffer

		reporter := newPresentation(&output, &output, time.Now(), func() {})

		setup := searchSetup{
			workers: 1,
			gpu:     gpuSetup{state: backendEnabled},
			config: search.Configuration{
				Requested: simd.BMI2ADX,
				Engine:    simd.BMI2ADX,
				Features:  simd.Features{Known: true, BMI2: true},
				Matching:  "scalar prefix",
			},
		}

		reporter.start(context.Background(), setup, matchEstimate{}, search.NewMonitor())

		text := output.String()

		warning := strings.Index(text, "forced backend not reported usable")
		if warning < 0 || warning > strings.Index(text, "Initializing GPU") || !strings.Contains(text, "scalar (bmi2+adx), forced") {
			t.Fatalf("forced choice was deferred until GPU selection: %s", text)
		}

		reporter.selectedGPU("Test GPU", 256, 4, time.Second, 2*time.Second)
		reporter.finish(runTotals{}, context.Canceled)

		text = output.String()
		if strings.Count(text, "Search setup") != 1 || strings.Count(text, "Warning") != 1 || !strings.Contains(text, "GPU selected: Test GPU, 256 streams, 4 rounds") {
			t.Fatalf("selection duplicated setup or lost GPU settings: %s", text)
		}
	})
}

func TestTuningLifecycle(t *testing.T) {
	modes := []string{"automatic", "manual", "cancelled"}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var (
					output  bytes.Buffer
					checked atomic.Uint64
				)

				monitor := search.NewMonitor()

				monitor.Observe(func() search.Stats {
					return search.Stats{Checked: checked.Load()}
				})

				reporter := newPresentation(&output, &output, time.Now(), func() {})

				setup := searchSetup{
					input:     patternInput{texts: []string{"rare."}},
					workers:   1,
					placement: "OS placement",
					output:    "results",
					gpu:       gpuSetup{state: backendEnabled, autoStreams: mode != "manual", autoRounds: mode != "manual"},
				}

				reporter.start(context.Background(), setup, newMatchEstimate(1.0/1024), monitor)

				time.Sleep(time.Second)

				reporter.initializedGPU("Test GPU", time.Second)
				reporter.readyGPU("Test GPU")

				checked.Store(500)

				time.Sleep(4 * time.Second)

				synctest.Wait()

				phase := "tuning"

				if mode == "manual" {
					phase = "validating"
				}

				if !strings.Contains(output.String(), "5s | "+phase+" | 500 checked | 100/s") || strings.Contains(output.String(), "Search setup") {
					t.Fatalf("unexplained preselection progress: %s", output.String())
				}

				if mode != "cancelled" {
					reporter.selectedGPU("Test GPU", 256, 4, time.Second, 5*time.Second)
				}

				checked.Store(1500)

				time.Sleep(5 * time.Second)

				synctest.Wait()

				totals := runTotals{cpu: backendTotals{state: backendEnabled}, gpu: backendTotals{stats: search.Stats{Checked: 1500}, state: backendEnabled, device: "Test GPU"}}

				reporter.finish(totals, context.Canceled)

				text := output.String()

				if strings.Count(text, "Search setup") != 1 || strings.Count(text, "Stopped:") != 1 || !strings.Contains(text, "1.5k checked | 200/s") || !strings.Contains(text, "Total  1,500 checked | 150 keys/s avg") {
					t.Fatalf("tuning work lost or counted twice: %s", text)
				}

				if mode == "cancelled" && (!strings.Contains(text, "configuration not selected") || !strings.Contains(text, "Stopped: interrupted")) {
					t.Fatalf("cancelled tuning misreported: %s", text)
				}

				if mode == "automatic" && !strings.Contains(text, "256 streams (auto), 4 rounds (auto)") {
					t.Fatalf("automatic settings missing: %s", text)
				}

				if mode == "manual" && !strings.Contains(text, "256 streams (explicit), 4 rounds (explicit)") {
					t.Fatalf("manual settings missing: %s", text)
				}
			})
		})
	}
}
