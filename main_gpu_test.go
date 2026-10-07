//go:build gpu

package main

import "testing"

func TestGPUProgressIntegration(t *testing.T) {
	// Timed checks deliberately run sequentially, GPU-only before mixed search.
	t.Run("GPU only", func(t *testing.T) {
		checkProgressIntegration(t, []string{"--gpu", "auto", "--cpu", "0"}, false)
	})

	t.Run("GPU plus one pinned CPU", func(t *testing.T) {
		checkProgressIntegration(t, []string{"--gpu", "auto", "--cpu", "1"}, true)
	})
}
