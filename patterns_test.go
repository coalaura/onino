package main

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

type patternFileCase struct {
	name      string
	text      string
	patterns  []string
	lines     []int
	wantError string
}

func TestPatternFile(t *testing.T) {
	cases := []patternFileCase{
		{name: "BOM CRLF comments whitespace duplicates", text: "\ufeff # comment\r\n\r\n  abc. \t\r\n  .def\r\nabc.\r\n", patterns: []string{"abc.", ".def", "abc."}, lines: []int{3, 4, 5}},
		{name: "LF without trailing newline", text: "abc.\n.def", patterns: []string{"abc.", ".def"}, lines: []int{1, 2}},
		{name: "spaces are not delimiters", text: "abc. def.", patterns: []string{"abc. def."}, lines: []int{1}},
		{name: "inline comments are literal", text: "abc. # comment", patterns: []string{"abc. # comment"}, lines: []int{1}},
		{name: "empty", wantError: "at least one pattern"},
		{name: "comments only", text: "\ufeff\n # comment\n\t", wantError: "at least one pattern"},
		{name: "invalid UTF8", text: "# comment\nabc.\n\xff", wantError: "line 3: invalid UTF-8"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, err := parsePatternFile("patterns with spaces.txt", test.text)

			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || !strings.Contains(err.Error(), "patterns with spaces.txt") {
					t.Fatalf("error = %v", err)
				}

				return
			}

			if err != nil || !reflect.DeepEqual(input.texts, test.patterns) || !reflect.DeepEqual(input.lines, test.lines) {
				t.Fatalf("input = %+v, error = %v", input, err)
			}
		})
	}
}

func TestPatternInputValidation(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "patterns with spaces.txt")

	writeTestFile(t, filename, "# comment\nabc.\n  abc. # inline\n")

	cases := [][]string{
		{"--patterns", filename},
		{"--patterns", filename, "abc."},
		{"--patterns", filename + ".missing"},
		{},
	}
	wants := []string{"line 3", "cannot be combined", "read patterns", "at least one pattern"}

	for index, flags := range cases {
		var output bytes.Buffer

		command := newCommand()
		command.Writer = &output
		command.ErrWriter = &output

		arguments := append([]string{"onino"}, flags...)

		err := command.Run(context.Background(), arguments)
		if err == nil || !strings.Contains(err.Error(), wants[index]) {
			t.Fatalf("%v: %v", flags, err)
		}

		if index == 0 && !strings.Contains(err.Error(), filename) {
			t.Fatalf("missing filename: %v", err)
		}

		if strings.Contains(output.String(), "Search setup") || strings.Contains(output.String(), "Initializing GPU") {
			t.Fatalf("started search before validation: %s", output.String())
		}
	}
}

func TestResolvedPatternsReadOnce(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "patterns.txt")

	writeTestFile(t, filename, "\ufeff# prefixes\r\nabc.\r\ndef.\r\nabc.\r\n")

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

		matcher, estimate, err := compileInput(input)
		if err != nil {
			return err
		}

		wantPatterns := []string{"abc.", "def.", "abc."}

		wantMatcher, err := pattern.CompilePatterns(wantPatterns)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(matcher, wantMatcher) || len(input.texts) != 3 {
			t.Fatal("file input changed matching or duplicate semantics")
		}

		probability, err := pattern.EstimateProbability(wantPatterns)
		if err != nil || estimate != newMatchEstimate(probability) {
			t.Fatalf("estimate changed: %+v, %v", estimate, err)
		}

		_, err = prepareBackend(command, input)

		return err
	}

	err := command.Run(context.Background(), []string{"onino", "--patterns", filename})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLargeAndRarestPatternInputs(t *testing.T) {
	// A full public key fixes its checksum. The existing model accounts for this
	// rather than treating those last four visible checksum bits as independent.
	var key onion.Key

	rarest := key.Hostname()[:52] + "."

	input, err := parsePatternFile("rare.txt", rarest)
	if err != nil {
		t.Fatal(err)
	}

	_, estimate, err := compileInput(input)
	if err != nil || estimate != newMatchEstimate(math.Ldexp(1, -256)) {
		t.Fatalf("rarest estimate = %+v, error = %v", estimate, err)
	}

	var text strings.Builder

	text.Grow(512 * 5)
	alphabet := "abcdefghijklmnopqrstuvwxyz234567"

	for index := range 512 {
		text.WriteByte('a')
		text.WriteByte(alphabet[index/32])
		text.WriteByte(alphabet[index%32])
		text.WriteString(".\n")
	}

	input, err = parsePatternFile("many.txt", text.String())
	if err != nil {
		t.Fatal(err)
	}

	_, estimate, err = compileInput(input)
	if err != nil || len(input.texts) != 512 || estimate != newMatchEstimate(1.0/64) {
		t.Fatalf("large collection = %d patterns, estimate %+v, error %v", len(input.texts), estimate, err)
	}
}

func TestOutputDirectorySelection(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	flags := []string{"--output", "-o"}

	for _, flag := range flags {
		var output bytes.Buffer

		directory := filepath.Join(t.TempDir(), "saved results", "nested")

		command := newCommand()
		command.Writer = &output
		command.ErrWriter = &output

		err := command.Run(ctx, []string{"onino", flag, directory, "rare."})
		if err != nil {
			t.Fatal(err)
		}

		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || strings.Count(output.String(), directory) != 2 {
			t.Fatalf("directory selection: %v\n%s", err, output.String())
		}

		if strings.ContainsAny(output.String(), "\r\x1b") {
			t.Fatal("redirected output contains terminal controls")
		}
	}

	filename := filepath.Join(t.TempDir(), "not a directory")

	writeTestFile(t, filename, "existing file")

	command := newCommand()

	err := command.Run(ctx, []string{"onino", "-o", filename, "rare."})
	if err == nil || !strings.Contains(err.Error(), filename) {
		t.Fatalf("directory creation error was lost: %v", err)
	}
}

func TestHelpAndOutputDefault(t *testing.T) {
	var output bytes.Buffer

	command := newCommand()
	command.Writer = &output

	err := command.Run(context.Background(), []string{"onino", "--help"})
	if err != nil {
		t.Fatal(err)
	}

	wants := []string{"onino --cpu all --gpu auto prefix.", "onino --cpu all --gpu auto --patterns patterns.txt --output results", "--patterns", "--output", "-o", "matches"}

	for _, want := range wants {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help missing %q: %s", want, output.String())
		}
	}

	command = newCommand()

	command.Action = func(ctx context.Context, command *cli.Command) error {
		if command.String("output") != "matches" {
			t.Fatal("default output directory changed")
		}

		return nil
	}

	err = command.Run(context.Background(), []string{"onino", "rare."})
	if err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, filename, text string) {
	t.Helper()

	err := os.WriteFile(filename, []byte(text), 0600)
	if err != nil {
		t.Fatal(err)
	}
}
