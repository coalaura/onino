package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/urfave/cli/v3"

	"github.com/coalaura/onino/internal/pattern"
)

type patternInput struct {
	texts []string
	lines []int
	file  string
}

func (input patternInput) describeError(err error, validate func([]string) error) error {
	if input.file == "" {
		return err
	}

	// Only revisit individual entries on failure, to attach physical file lines
	// without changing either compiler's validation or duplicate semantics.
	for index := range input.texts {
		entryError := validate(input.texts[index : index+1])
		if entryError != nil {
			return fmt.Errorf("patterns %s, line %d: %w", input.file, input.lines[index], entryError)
		}
	}

	return fmt.Errorf("patterns %s: %w", input.file, err)
}

func resolvePatterns(command *cli.Command) (patternInput, error) {
	if command.IsSet("patterns") {
		if command.NArg() != 0 {
			return patternInput{}, fmt.Errorf("--patterns cannot be combined with positional patterns")
		}

		filename := command.String("patterns")

		data, err := os.ReadFile(filename)
		if err != nil {
			return patternInput{}, fmt.Errorf("read patterns %s: %w", filename, err)
		}

		return parsePatternFile(filename, string(data))
	}

	texts := command.Args().Slice()
	if len(texts) == 0 {
		return patternInput{}, fmt.Errorf("at least one pattern is required (positional patterns or --patterns file)")
	}

	return patternInput{texts: texts}, nil
}

func parsePatternFile(filename, text string) (patternInput, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	capacity := strings.Count(text, "\n") + 1

	input := patternInput{texts: make([]string, 0, capacity), lines: make([]int, 0, capacity), file: filename}

	lineNumber := 0

	for line := range strings.SplitSeq(text, "\n") {
		lineNumber++

		if !utf8.ValidString(line) {
			return patternInput{}, fmt.Errorf("patterns %s, line %d: invalid UTF-8", filename, lineNumber)
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		input.texts = append(input.texts, line)
		input.lines = append(input.lines, lineNumber)
	}

	if len(input.texts) == 0 {
		return patternInput{}, fmt.Errorf("patterns %s: at least one pattern is required", filename)
	}

	return input, nil
}

func compileInput(input patternInput) (*pattern.Matcher, matchEstimate, error) {
	matcher, err := pattern.CompilePatterns(input.texts)
	if err != nil {
		return nil, matchEstimate{}, input.describeError(err, validatePatterns)
	}

	probability, err := pattern.EstimateProbability(input.texts)
	if err != nil {
		return nil, matchEstimate{}, input.describeError(err, validateProbability)
	}

	return matcher, newMatchEstimate(probability), nil
}

func validatePatterns(texts []string) error {
	_, err := pattern.CompilePatterns(texts)

	return err
}

func validateProbability(texts []string) error {
	_, err := pattern.EstimateProbability(texts)

	return err
}
