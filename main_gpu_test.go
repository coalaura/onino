//go:build gpu

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/gpu"
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
				input, err := resolvePatterns(command)
				if err != nil {
					return err
				}

				_, err = prepareBackend(command, input)
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

func TestGPUUsesResolvedFilePatterns(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "gpu patterns.txt")

	writeTestFile(t, filename, "\ufeff# filters\r\nabc.\r\ndef.\r\nabc.\r\n")

	command := newCommand()

	command.Action = func(ctx context.Context, command *cli.Command) error {
		input, err := resolvePatterns(command)
		if err != nil {
			return err
		}

		err = os.Remove(filename)
		if err != nil {
			t.Fatal(err)
		}

		_, estimate, err := compileInput(input)
		if err != nil {
			return err
		}

		backend, err := prepareBackend(command, input)
		if err != nil {
			return err
		}

		want, err := gpu.Compile([]string{"abc.", "def.", "abc."})
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(backend.plan, want) || estimate != newMatchEstimate(2.0/32768) || !backend.options.AutoStreams || !backend.options.AutoRounds {
			t.Fatal("GPU plan, estimation, or tuning used a different pattern source")
		}

		return nil
	}

	err := command.Run(context.Background(), []string{"onino", "--gpu", "auto", "--patterns", filename})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGPUFileValidationBeforeInitialization(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "gpu patterns.txt")

	writeTestFile(t, filename, "# filters\nabc.\n.def\n")

	var output bytes.Buffer

	command := newCommand()
	command.Writer = &output
	command.ErrWriter = &output

	err := command.Run(context.Background(), []string{"onino", "--gpu", "auto", "--patterns", filename})
	if err == nil || !strings.Contains(err.Error(), filename) || !strings.Contains(err.Error(), "line 3") || strings.Contains(output.String(), "Initializing") {
		t.Fatalf("GPU invalid entry: %v\n%s", err, output.String())
	}

	writeTestFile(t, filename, strings.Repeat("abc.\n", 9))

	command = newCommand()
	command.Writer = &output
	command.ErrWriter = &output

	err = command.Run(context.Background(), []string{"onino", "--gpu", "auto", "--patterns", filename})
	if err == nil || !strings.Contains(err.Error(), filename) || !strings.Contains(err.Error(), "one to eight anchored prefixes") {
		t.Fatalf("GPU duplicate/collection limit changed: %v", err)
	}
}
