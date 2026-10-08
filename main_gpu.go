//go:build gpu

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/gpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

type backendResult struct {
	totals backendTotals
	gpu    bool
	err    error
}

type backendPlan struct {
	enabled     bool
	diagnostics bool
	plan        gpu.Plan
	options     gpu.Options
}

func (backend backendPlan) settings() gpuSetup {
	state := backendDisabled

	if backend.enabled {
		state = backendEnabled
	}

	return gpuSetup{state: state, autoStreams: backend.options.AutoStreams, autoRounds: backend.options.AutoRounds}
}

func backendFlags(flags []cli.Flag) []cli.Flag {
	for _, flag := range flags {
		if cpuFlag, ok := flag.(*cli.StringFlag); ok && cpuFlag.Name == "cpu" {
			cpuFlag.Usage += "; 0 or off disables CPU search when GPU is enabled"
		}
	}

	return append(flags, &cli.StringFlag{
		Name:  "gpu",
		Value: "off",
		Usage: "Vulkan prefix search: off, auto, or physical device index; --cpu 0 or --cpu off selects GPU only",
	}, &cli.IntFlag{
		Name:        "gpu-streams",
		Value:       gpu.DefaultStreams,
		DefaultText: "auto",
		Usage:       "Fix GPU resident streams (1-16384); omitted values are calibrated during real search",
	}, &cli.IntFlag{
		Name:        "gpu-rounds",
		Value:       gpu.DefaultRounds,
		DefaultText: "auto",
		Usage:       "Fix GPU rounds per submission (1-64); omitted values are calibrated during real search",
	}, &cli.BoolFlag{
		Name:  "gpu-diagnostics",
		Usage: "Print GPU allocation, tuning and per-second accounting diagnostics",
	})
}

func prepareBackend(command *cli.Command, input patternInput) (backendPlan, error) {
	if command.String("gpu") == "off" {
		if command.IsSet("gpu-streams") || command.IsSet("gpu-rounds") {
			return backendPlan{}, errors.New("--gpu-streams and --gpu-rounds require an enabled --gpu")
		}

		return backendPlan{}, nil
	}

	options, err := gpuOptions(command)
	if err != nil {
		return backendPlan{}, err
	}

	plan, err := gpu.Compile(input.texts)
	if err != nil {
		return backendPlan{}, input.describeError(err, validateGPUPatterns)
	}

	return backendPlan{enabled: true, diagnostics: command.Bool("gpu-diagnostics"), plan: plan, options: options}, nil
}

func validateGPUPatterns(texts []string) error {
	_, err := gpu.Compile(texts)

	return err
}

func resolveWorkers(command *cli.Command, available int) (int, error) {
	selection := command.String("cpu")
	if command.String("gpu") != "off" && (selection == "0" || selection == "off") {
		return 0, nil
	}

	return cpu.Resolve(selection, available)
}

func backendParallelism(command *cli.Command, workers int) int {
	if command.String("gpu") != "off" {
		return workers + 2
	}

	return workers
}

func pinSingleWorker(command *cli.Command, workers int) bool {
	return command.String("gpu") != "off" && workers == 1
}

func runBackend(ctx context.Context, backend backendPlan, matcher *pattern.Matcher, save search.SaveMatchFunc, options search.Options, reporter *presentation) (runTotals, error) {
	if !backend.enabled {
		totals, err := runCPU(ctx, matcher, save, options)

		return runTotals{cpu: totals, gpu: backendTotals{state: backendDisabled}}, err
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		callbacks sync.Mutex
		saveError error
	)

	serializedSave := func(key onion.Key, found time.Time) error {
		callbacks.Lock()
		defer callbacks.Unlock()

		if saveError != nil {
			return saveError
		}

		saveError = save(key, found)
		if saveError != nil {
			options.Monitor.Stop()
			cancel()
		}

		return saveError
	}

	results := make(chan backendResult, 2)
	backends := 1

	if options.Workers > 0 {
		backends++

		go func() {
			totals, runError := runCPU(workCtx, matcher, serializedSave, options)
			options.Monitor.Stop()
			cancel()
			results <- backendResult{totals: totals, err: runError}
		}()
	}

	go func() {
		deviceOptions := backend.options
		deviceOptions.Monitor = options.Monitor

		if backend.diagnostics {
			deviceOptions.Diagnostic = reporter.diagnostic
		}

		deviceOptions.Started = reporter.started
		deviceOptions.Initialized = reporter.initializedGPU
		deviceOptions.Ready = reporter.readyGPU
		deviceOptions.Selected = reporter.selectedGPU

		stats, metrics, runError := gpu.Run(workCtx, backend.plan, matcher, serializedSave, deviceOptions)
		options.Monitor.Stop()

		state := backendUnavailable

		if metrics.Device != "" {
			state = backendEnabled
		}

		totals := backendTotals{stats: stats, state: state, device: metrics.Device}
		cancel()

		results <- backendResult{totals: totals, gpu: true, err: runError}
	}()

	var (
		runError error
	)

	total := runTotals{cpu: backendTotals{state: backendDisabled}}

	for range backends {
		result := <-results
		if result.gpu {
			total.gpu = result.totals
		} else {
			total.cpu = result.totals
		}

		if result.err != nil && (runError == nil || errors.Is(runError, context.Canceled)) {
			runError = result.err
		}
	}

	if saveError != nil {
		return total, saveError
	}

	return total, runError
}

func gpuOptions(command *cli.Command) (gpu.Options, error) {
	index, err := gpuIndex(command.String("gpu"))
	if err != nil {
		return gpu.Options{}, err
	}

	streams := command.Int("gpu-streams")
	if streams < 1 || streams > gpu.MaxStreams {
		return gpu.Options{}, fmt.Errorf("invalid --gpu-streams %d: expected 1-%d", streams, gpu.MaxStreams)
	}

	rounds := command.Int("gpu-rounds")
	if rounds < 1 || rounds > gpu.MaxRounds {
		return gpu.Options{}, fmt.Errorf("invalid --gpu-rounds %d: expected 1-%d", rounds, gpu.MaxRounds)
	}

	return gpu.Options{Device: index, Streams: streams, Rounds: rounds, AutoStreams: !command.IsSet("gpu-streams"), AutoRounds: !command.IsSet("gpu-rounds")}, nil
}

func gpuIndex(value string) (int, error) {
	if value == "auto" {
		return -1, nil
	}

	index, err := strconv.Atoi(value)
	if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)>>1) {
		return 0, fmt.Errorf("invalid --gpu %q: expected off, auto, or a device index", value)
	}

	return index, nil
}
