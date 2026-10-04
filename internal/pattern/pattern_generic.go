//go:build !amd64

package pattern

const vectorAvailable = false

// Non-amd64 compilation uses scalar matching exclusively.
func filterCandidates(data *[32]byte, plans *scanPlan, count int) (int, uint64) {
	panic("vector filtering is unavailable on this architecture")
}
