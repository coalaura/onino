//go:build !amd64 || purego

package search

import "testing"

func checkWorkerMode(t *testing.T, state *worker, config Configuration) {
	t.Helper()

	if config.Independent {
		if state.walk == nil || state.walk.fieldMode != config.Engine {
			t.Fatal("incorrect portable independent walk")
		}
	} else if state.paired == nil || state.paired.fieldMode != config.Engine {
		t.Fatal("incorrect portable paired generator")
	}
}
