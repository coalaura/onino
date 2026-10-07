package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
	"github.com/coalaura/onino/internal/simd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := newCommand().Run(ctx, os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCommand() *cli.Command {
	return &cli.Command{
		Name:        "onino",
		Usage:       "Continuously search for vanity v3 onion addresses",
		ArgsUsage:   "pattern [pattern ...]",
		Description: "Patterns match the first 52 visible lowercase base32 characters of the onion hostname.\nSuffixes end at character 52, before the final four checksum/version characters.\nForms: prefix.  .suffix  prefix.suffix  .interior.  anywhere",
		Flags: backendFlags([]cli.Flag{
			&cli.StringFlag{
				Name:  "simd",
				Value: "auto",
				Usage: "Optional SIMD: auto or avx2 (disable all onino AVX-512 paths)",
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
	if command.NArg() == 0 {
		return errors.New("at least one pattern is required")
	}

	err := validateBackend(command)
	if err != nil {
		return err
	}

	mode, err := simd.Parse(command.String("simd"))
	if err != nil {
		return err
	}

	matcher, err := pattern.CompilePatterns(command.Args().Slice())
	if err != nil {
		return err
	}

	probability, err := pattern.EstimateProbability(command.Args().Slice())
	if err != nil {
		return err
	}

	estimate := newMatchEstimate(probability)

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

	fmt.Fprintf(command.ErrWriter, "Searching with %d worker(s), %s; saving matches to %s. Press Ctrl+C to stop.\n", workers, placement, output)

	var progressBuffer [320]byte

	command.ErrWriter.Write(appendCandidateEstimate(progressBuffer[:0], estimate))

	started := time.Now()
	lastMatch := started

	var outputMutex sync.Mutex

	command.Writer = serializedWriter{writer: command.Writer, mutex: &outputMutex}
	command.ErrWriter = serializedWriter{writer: command.ErrWriter, mutex: &outputMutex}

	monitor := search.NewMonitor()
	reporter := startProgress(ctx, command.ErrWriter, estimate, monitor, started)

	options := search.Options{Workers: workers, CPUs: processors, Monitor: monitor, SIMD: mode}

	stats, err := runBackend(ctx, command, matcher, func(key onion.Key, found time.Time) error {
		saveError := store.Save(key)
		if saveError != nil {
			return saveError
		}

		// Queue order across workers need not be discovery-time order.
		matchSeconds := max(0, found.Sub(lastMatch).Seconds())
		totalSeconds := found.Sub(started).Seconds()

		fmt.Fprintf(command.Writer, "%s in %.2fs (%.2fs total)\n", key.Hostname(), matchSeconds, totalSeconds)

		if found.After(lastMatch) {
			lastMatch = found
		}

		return nil
	}, options)

	reporter.finish(stats)

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}
