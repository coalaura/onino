package main

import "github.com/coalaura/onino/internal/pattern"

func main() {
	_, err := pattern.CompilePatterns([]string{
		"start.enda",
		".middle.",
		"begin.",
		".finisha",
		"anywhere",
	})

	if err != nil {
		panic(err)
	}
}
