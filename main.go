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

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

func main() {
	runtime.GOMAXPROCS(1)

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
		Usage:       "Continuously search for vanity v3 onion addresses with one CPU worker",
		ArgsUsage:   "pattern [pattern ...]",
		Description: "Patterns match the 32-byte public key encoded as lowercase base32.\nForms: prefix.  .suffix  prefix.suffix  .interior.  anywhere",
		Flags: []cli.Flag{
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

	output := command.String("output")

	store, err := onion.NewStore(output)
	if err != nil {
		return err
	}

	fmt.Fprintf(command.ErrWriter, "Searching with one worker; saving matches to %s. Press Ctrl+C to stop.\n", output)

	started := time.Now()

	stats, err := search.Run(ctx, matcher, func(key onion.Key) error {
		saveError := store.Save(key)
		if saveError != nil {
			return saveError
		}

		fmt.Fprintln(command.Writer, key.Hostname())

		return nil
	})

	elapsed := time.Since(started)

	fmt.Fprintf(command.ErrWriter, "Checked %d keys, saved %d matches in %s (%.0f keys/s).\n", stats.Checked, stats.Saved, elapsed.Round(time.Millisecond), float64(stats.Checked)/elapsed.Seconds())

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}
