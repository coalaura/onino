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
	stats search.Stats
	err   error
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
		Name:  "gpu-streams",
		Value: gpu.DefaultStreams,
		Usage: "GPU resident streams (1-16384); requires --gpu",
	}, &cli.IntFlag{
		Name:  "gpu-rounds",
		Value: gpu.DefaultRounds,
		Usage: "GPU rounds per dispatch (1-64); requires --gpu",
	})
}

func validateBackend(command *cli.Command) error {
	if command.String("gpu") == "off" {
		if command.IsSet("gpu-streams") || command.IsSet("gpu-rounds") {
			return errors.New("--gpu-streams and --gpu-rounds require an enabled --gpu")
		}

		return nil
	}

	_, err := gpuOptions(command)
	if err != nil {
		return err
	}

	_, err = gpu.Compile(command.Args().Slice())

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

func runBackend(ctx context.Context, command *cli.Command, matcher *pattern.Matcher, save search.SaveMatchFunc, options search.Options) (search.Stats, error) {
	if command.String("gpu") == "off" {
		return search.RunQueued(ctx, matcher, save, options)
	}

	deviceOptions, err := gpuOptions(command)
	if err != nil {
		return search.Stats{}, err
	}

	plan, err := gpu.Compile(command.Args().Slice())
	if err != nil {
		return search.Stats{}, err
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
			stats, runError := search.RunQueued(workCtx, matcher, serializedSave, options)
			options.Monitor.Stop()
			results <- backendResult{stats: stats, err: runError}
			cancel()
		}()
	}

	go func() {
		deviceOptions.Monitor = options.Monitor

		deviceOptions.Ready = func(device string) {
			fmt.Fprintf(command.ErrWriter, "GPU %s: %d streams, %d rounds.\n", device, deviceOptions.Streams, deviceOptions.Rounds)
		}

		stats, metrics, runError := gpu.Run(workCtx, plan, matcher, serializedSave, deviceOptions)
		options.Monitor.Stop()

		callbacks.Lock()

		if metrics.Execution > 0 {
			fmt.Fprintf(command.ErrWriter, "\nGPU %s: %d candidates, %.3f M/s device, %.3f M/s end-to-end, %d submissions.\n", metrics.Device, stats.Checked, float64(stats.Checked)/metrics.Execution.Seconds()/1e6, float64(stats.Checked)/metrics.Elapsed.Seconds()/1e6, metrics.Submissions)
		}

		callbacks.Unlock()

		results <- backendResult{stats: stats, err: runError}
		cancel()
	}()

	var (
		total    search.Stats
		runError error
	)

	for range backends {
		result := <-results
		total = sumStats(total, result.stats)

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

	return gpu.Options{Device: index, Streams: streams, Rounds: rounds}, nil
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

func sumStats(first, second search.Stats) search.Stats {
	return search.Stats{Checked: first.Checked + second.Checked, Saved: first.Saved + second.Saved}
}
