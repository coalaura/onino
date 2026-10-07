//go:build gpu

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

type gpuFlagCase struct {
	name    string
	flags   []string
	streams int
	rounds  int
	wantErr string
}

func TestGPUConfigurationFlags(t *testing.T) {
	cases := []gpuFlagCase{
		{name: "defaults", streams: 256, rounds: 4},
		{name: "fixed streams", flags: []string{"--gpu-streams", "4096"}, streams: 4096, rounds: 4},
		{name: "fixed rounds", flags: []string{"--gpu-rounds", "2"}, streams: 256, rounds: 2},
		{name: "rare prefix setting", flags: []string{"--gpu-streams", "4096", "--gpu-rounds", "2"}, streams: 4096, rounds: 2},
		{name: "minimum", flags: []string{"--gpu-streams", "1", "--gpu-rounds", "1"}, streams: 1, rounds: 1},
		{name: "maximum", flags: []string{"--gpu-streams", "16384", "--gpu-rounds", "64"}, streams: 16384, rounds: 64},
		{name: "zero streams", flags: []string{"--gpu-streams", "0"}, wantErr: "invalid --gpu-streams"},
		{name: "negative streams", flags: []string{"--gpu-streams", "-1"}, wantErr: "invalid --gpu-streams"},
		{name: "too many streams", flags: []string{"--gpu-streams", "16385"}, wantErr: "invalid --gpu-streams"},
		{name: "zero rounds", flags: []string{"--gpu-rounds", "0"}, wantErr: "invalid --gpu-rounds"},
		{name: "negative rounds", flags: []string{"--gpu-rounds", "-1"}, wantErr: "invalid --gpu-rounds"},
		{name: "too many rounds", flags: []string{"--gpu-rounds", "65"}, wantErr: "invalid --gpu-rounds"},
		{name: "fractional streams", flags: []string{"--gpu-streams", "2.5"}, wantErr: "invalid"},
		{name: "invalid rounds", flags: []string{"--gpu-rounds", "invalid"}, wantErr: "invalid"},
		{name: "overflow", flags: []string{"--gpu-streams", "999999999999999999999"}, wantErr: "out of range"},
		{name: "streams without gpu", flags: []string{"--gpu", "off", "--gpu-streams", "256"}, wantErr: "require an enabled --gpu"},
		{name: "rounds without gpu", flags: []string{"--gpu", "off", "--gpu-rounds", "4"}, wantErr: "require an enabled --gpu"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			command := newCommand()

			output := new(bytes.Buffer)

			command.Writer = output
			command.ErrWriter = output

			command.Action = func(ctx context.Context, command *cli.Command) error {
				err := validateBackend(command)
				if err != nil {
					return err
				}

				options, err := gpuOptions(command)
				if err != nil {
					return err
				}

				if options.Device != -1 || options.Streams != test.streams || options.Rounds != test.rounds {
					t.Fatalf("resolved options: %+v", options)
				}

				if options.AutoStreams == command.IsSet("gpu-streams") || options.AutoRounds == command.IsSet("gpu-rounds") {
					t.Fatalf("explicit flags must fix only their own dimension: %+v", options)
				}

				return nil
			}

			arguments := []string{"onino", "--gpu", "auto", "--cpu", "off"}
			arguments = append(arguments, test.flags...)
			arguments = append(arguments, "chopperepic.")

			err := command.Run(context.Background(), arguments)

			if test.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func TestGPUProgressIntegration(t *testing.T) {
	// Timed checks deliberately run sequentially, GPU-only before mixed search.
	t.Run("GPU only", func(t *testing.T) {
		checkProgressIntegration(t, []string{"--gpu", "auto", "--cpu", "off", "--gpu-streams", "4096", "--gpu-rounds", "2"}, false)
	})

	t.Run("GPU plus one pinned CPU", func(t *testing.T) {
		checkProgressIntegration(t, []string{"--gpu", "auto", "--cpu", "1", "--gpu-streams", "4096", "--gpu-rounds", "2"}, true)
	})
}
