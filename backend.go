package main

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

const (
	backendDisabled    = "disabled"
	backendUnavailable = "unavailable (not initialized)"
	backendEnabled     = "enabled"
)

type backendTotals struct {
	stats  search.Stats
	state  string
	device string
}

type runTotals struct {
	cpu backendTotals
	gpu backendTotals
}

type gpuSetup struct {
	state          string
	device         string
	streams        int
	rounds         int
	autoStreams    bool
	autoRounds     bool
	initialization time.Duration
	first          time.Duration
	selection      time.Duration
}

func (totals runTotals) combined() search.Stats {
	return search.Stats{Checked: totals.cpu.stats.Checked + totals.gpu.stats.Checked, Saved: totals.cpu.stats.Saved + totals.gpu.stats.Saved}
}

func runCPU(ctx context.Context, matcher *pattern.Matcher, save search.SaveMatchFunc, options search.Options) (backendTotals, error) {
	var ready atomic.Bool

	options.Ready = func() {
		ready.Store(true)
	}

	stats, err := search.RunQueued(ctx, matcher, save, options)

	state := backendUnavailable

	if ready.Load() {
		state = backendEnabled
	}

	return backendTotals{stats: stats, state: state}, err
}
