package main

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestCPUFlag(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	values := []string{"", "1", "all", "0", "-1", "2.5", "invalid", "99999999999999999"}

	for _, value := range values {
		command := newCommand()

		output := new(bytes.Buffer)

		command.Writer = output
		command.ErrWriter = output

		arguments := []string{"onino", "--output", t.TempDir()}

		if value != "" {
			arguments = append(arguments, "--cpu", value)
		}

		arguments = append(arguments, "rare.")
		wantSuccess := value == "" || value == "1" || value == "all"

		err := command.Run(ctx, arguments)
		if (err == nil) != wantSuccess {
			t.Fatalf("--cpu %q: %v", value, err)
		}

		if value == "" && (!strings.Contains(output.String(), "1 worker(s), OS placement") || runtime.GOMAXPROCS(0) != 1) {
			t.Fatal("default did not preserve direct single-worker execution")
		}
	}
}
