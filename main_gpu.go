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
	})
}

func validateBackend(command *cli.Command) error {
	if command.String("gpu") == "off" {
		return nil
	}

	_, err := gpuIndex(command.String("gpu"))
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

	index, err := gpuIndex(command.String("gpu"))
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
		callbacks     sync.Mutex
		progressStats [2]search.Stats
		saveError     error
	)

	serializedSave := func(key onion.Key, found time.Time) error {
		callbacks.Lock()
		defer callbacks.Unlock()

		if saveError != nil {
			return saveError
		}

		saveError = save(key, found)
		if saveError != nil {
			cancel()
		}

		return saveError
	}

	progress := options.Progress

	report := func(index int, stats search.Stats) {
		callbacks.Lock()
		defer callbacks.Unlock()

		progressStats[index] = stats

		if progress != nil {
			progress(sumStats(progressStats[0], progressStats[1]))
		}
	}

	results := make(chan backendResult, 2)
	backends := 1

	if options.Workers > 0 {
		backends++

		options.Progress = func(stats search.Stats) {
			report(0, stats)
		}

		go func() {
			stats, runError := search.RunQueued(workCtx, matcher, serializedSave, options)
			report(0, stats)
			results <- backendResult{stats: stats, err: runError}
			cancel()
		}()
	}

	go func() {
		gpuProgress := func(stats search.Stats) {
			report(1, stats)
		}

		gpuOptions := gpu.Options{Device: index, Progress: gpuProgress}

		stats, metrics, runError := gpu.Run(workCtx, plan, matcher, serializedSave, gpuOptions)
		report(1, stats)

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
