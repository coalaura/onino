package main

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
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

		if wantSuccess && !strings.Contains(output.String(), "Candidates  50% ~") {
			t.Fatal("startup did not show combined match estimates")
		}
	}
}

func TestSIMDFlag(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	values := []string{"auto", "avx2", "", "avx512", "AUTO"}

	for _, value := range values {
		command := newCommand()

		output := new(bytes.Buffer)

		command.Writer = output
		command.ErrWriter = output

		arguments := []string{"onino", "--simd", value, "--output", t.TempDir(), "rare."}

		err := command.Run(ctx, arguments)
		valid := value == "auto" || value == "avx2"

		if (err == nil) != valid {
			t.Fatalf("--simd %q: %v", value, err)
		}
	}
}

func TestCPUProgressIntegration(t *testing.T) {
	checkProgressIntegration(t, []string{"--cpu", "1"}, false)
}

func checkProgressIntegration(t *testing.T, flags []string, pinned bool) {
	t.Helper()

	if os.Getenv("ONINO_PROGRESS_TEST") != "1" {
		t.Skip("set ONINO_PROGRESS_TEST=1 for timed CLI progress checks")
	}

	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timer := time.AfterFunc(13*time.Second, cancel)
	defer timer.Stop()

	var output bytes.Buffer

	command := newCommand()
	command.Writer = &output
	command.ErrWriter = &output

	arguments := []string{"onino", "--output", t.TempDir()}
	arguments = append(arguments, flags...)
	arguments = append(arguments, "somethingrare.")

	err := command.Run(ctx, arguments)
	if err != nil {
		t.Fatal(err)
	}

	text := output.String()
	t.Log(text)

	if strings.Count(text, " | wait ") != 2 || strings.Count(text, "Stopped:") != 1 {
		t.Fatal("expected two shared periodic reports and one final summary")
	}

	if strings.Contains(text, "NaN") || strings.Contains(text, "+Inf") || strings.Contains(text, "Total  0 checked") {
		t.Fatal("invalid throughput or missing completed work")
	}

	if pinned && !strings.Contains(text, "1 worker(s), pinning physical cores first") {
		t.Fatal("mixed check requires exactly one pinned CPU search worker")
	}

	if command.String("gpu") == "auto" && strings.Count(text, "4096 streams (explicit), 2 rounds (explicit)") != 1 {
		t.Fatal("expected selected GPU configuration exactly once at startup")
	}
}
