//go:build !windows

package onion

import "os"

func publishMatch(staging, destination string) error {
	return os.Rename(staging, destination)
}
