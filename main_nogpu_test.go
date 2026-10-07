//go:build !gpu

package main

import "testing"

func TestGPUFlagsExcluded(t *testing.T) {
	for _, flag := range newCommand().Flags {
		for _, name := range flag.Names() {
			if name == "gpu" || name == "gpu-streams" || name == "gpu-rounds" {
				t.Fatalf("GPU flag %q exposed by ordinary build", name)
			}
		}
	}
}
