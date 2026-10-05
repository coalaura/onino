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
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
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
		Description: "Patterns match the 32-byte public key encoded as lowercase base32.\nForms: prefix.  .suffix  prefix.suffix  .interior.  anywhere",
		Flags: []cli.Flag{
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
		},
		Action: runSearch,
	}
}

func runSearch(ctx context.Context, command *cli.Command) error {
	if command.NArg() == 0 {
		return errors.New("at least one pattern is required")
	}

	matcher, err := pattern.CompilePatterns(command.Args().Slice())
	if err != nil {
		return err
	}

	topology, err := cpu.Discover()
	if err != nil {
		return err
	}

	workers, err := cpu.Resolve(command.String("cpu"), len(topology.CPUs))
	if err != nil {
		return err
	}

	runtime.GOMAXPROCS(workers)

	var processors []cpu.CPU

	placement := "OS placement"

	if workers > 1 {
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

	started := time.Now()
	lastReport := started

	var (
		lastChecked    uint64
		progressBuffer [192]byte
	)

	progress := func(stats search.Stats) {
		now := time.Now()
		rate := float64(stats.Checked-lastChecked) / now.Sub(lastReport).Seconds()

		lastReport = now
		lastChecked = stats.Checked

		// Reuse the line buffer so periodic reporting does not allocate.
		elapsed := now.Sub(started).Truncate(time.Second)

		line := appendSearchStatus(progressBuffer[:0], stats, elapsed, rate, false)

		command.ErrWriter.Write(line)
	}

	options := search.Options{Workers: workers, CPUs: processors, Progress: progress}

	stats, err := search.RunWithOptions(ctx, matcher, func(key onion.Key) error {
		saveError := store.Save(key)
		if saveError != nil {
			return saveError
		}

		fmt.Fprintln(command.Writer, key.Hostname())

		return nil
	}, options)

	elapsed := time.Since(started)
	rate := float64(stats.Checked) / elapsed.Seconds()

	line := appendSearchStatus(progressBuffer[:0], stats, elapsed.Round(time.Millisecond), rate, true)

	command.ErrWriter.Write(line)

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}
