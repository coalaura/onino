//go:build !gpu

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

func backendFlags(flags []cli.Flag) []cli.Flag {
	return flags
}

func validateBackend(command *cli.Command) error {
	return nil
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

func runBackend(ctx context.Context, command *cli.Command, matcher *pattern.Matcher, save search.SaveMatchFunc, options search.Options) (search.Stats, error) {
	return search.RunQueued(ctx, matcher, save, options)
}
