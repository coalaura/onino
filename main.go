package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/search"
	"github.com/coalaura/onino/internal/simd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := newCommand().Run(ctx, os.Args)
	if err != nil {
		_, reported := errors.AsType[reportedError](err)
		if !reported {
			fmt.Fprintln(os.Stderr, err)
		}

		os.Exit(1)
	}
}

func newCommand() *cli.Command {
	return &cli.Command{
		Name:        "onino",
		Usage:       "Continuously search for vanity v3 onion addresses",
		ArgsUsage:   "[pattern ...]",
		UsageText:   "onino --cpu all --gpu auto prefix.\n   onino --cpu all --gpu auto --patterns patterns.txt --output results",
		Description: "Patterns match the first 52 visible lowercase base32 characters of the onion hostname.\nSuffixes end at character 52, before the final four checksum/version characters.\nForms: prefix.  .suffix  prefix.suffix  .interior.  anywhere\nGPU options require a GPU-enabled build; omit --gpu for CPU-only builds.",
		Flags: backendFlags([]cli.Flag{
			&cli.StringFlag{
				Name:  "patterns",
				Usage: "Read one pattern per line from a UTF-8 file (blank lines and # comment lines ignored)",
			},
			&cli.StringFlag{
				Name:  "simd",
				Value: "auto",
				Usage: "CPU arithmetic: auto, portable, bmi2, bmi2-adx or ifma (explicit choices override detection)",
			},
			&cli.StringFlag{
				Name:  "cpu",
				Value: "1",
				Usage: "Search workers: a positive integer or all available logical CPUs",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Value:   "matches",
				Usage:   "Directory for matching Tor service keys",
			},
		}),
		Action: runSearch,
	}
}

func runSearch(ctx context.Context, command *cli.Command) error {
	started := time.Now()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	reporter := newPresentation(command.Writer, command.ErrWriter, started, cancel)

	if command.IsSet("patterns") && command.NArg() == 0 {
		reporter.loading(command.String("patterns"))
	}

	input, err := resolvePatterns(command)
	if err != nil {
		return err
	}

	matcher, estimate, err := compileInput(input)
	if err != nil {
		return err
	}

	backend, err := prepareBackend(command, input)
	if err != nil {
		return err
	}

	mode, err := simd.Parse(command.String("simd"))
	if err != nil {
		return err
	}

	config, err := search.Resolve(mode, matcher)
	if err != nil {
		return err
	}

	topology, err := cpu.Discover()
	if err != nil {
		return err
	}

	workers, err := resolveWorkers(command, len(topology.CPUs))
	if err != nil {
		return err
	}

	runtime.GOMAXPROCS(backendParallelism(command, workers))

	var processors []cpu.CPU

	placement := "OS placement"

	if workers > 1 || pinSingleWorker(command, workers) {
		if topology.Known {
			processors, err = cpu.Select(topology, workers, true)
			if err != nil {
				return err
			}

			placement = "pinning physical cores first, spread across caches"
		} else {
			placement = "OS placement (topology/affinity unavailable)"
		}
	}

	output := command.String("output")

	store, err := onion.NewStore(output)
	if err != nil {
		return err
	}

	monitor := search.NewMonitor()

	setup := searchSetup{
		input:     input,
		workers:   workers,
		placement: placement,
		output:    output,
		prepared:  time.Since(started),
		gpu:       backend.settings(),
		config:    config,
	}

	reporter.start(ctx, setup, estimate, monitor)

	options := search.Options{Workers: workers, CPUs: processors, Monitor: monitor, SIMD: mode, Config: &config}

	totals, err := runBackend(ctx, backend, matcher, func(key onion.Key, found time.Time) error {
		saveError := store.Save(key)
		if saveError != nil {
			return saveError
		}

		reporter.saved(key, found)

		return nil
	}, options, reporter)

	return reporter.finish(totals, err)
}
