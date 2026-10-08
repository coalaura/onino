//go:build !gpu

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

type backendPlan struct{}

func (backend backendPlan) settings() gpuSetup {
	return gpuSetup{state: "unavailable (not built)"}
}

func backendFlags(flags []cli.Flag) []cli.Flag {
	return flags
}

func prepareBackend(command *cli.Command, input patternInput) (backendPlan, error) {
	return backendPlan{}, nil
}

func resolveWorkers(command *cli.Command, available int) (int, error) {
	return cpu.Resolve(command.String("cpu"), available)
}

func backendParallelism(command *cli.Command, workers int) int {
	return workers
}

func pinSingleWorker(command *cli.Command, workers int) bool {
	return false
}

func runBackend(ctx context.Context, backend backendPlan, matcher *pattern.Matcher, save search.SaveMatchFunc, options search.Options, reporter *presentation) (runTotals, error) {
	totals, err := runCPU(ctx, matcher, save, options)

	return runTotals{cpu: totals, gpu: backendTotals{state: backend.settings().state}}, err
}
