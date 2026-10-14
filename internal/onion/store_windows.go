package onion

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	publishRetryTimeout                 = 2 * time.Second
	errorSharingViolation syscall.Errno = 32
)

func publishMatch(staging, destination string) error {
	err := os.Rename(staging, destination)
	if !retryablePublishError(err) {
		return err
	}

	// Windows readers without FILE_SHARE_DELETE can briefly block the directory
	// rename even after our file handles close. Keep the fully flushed staging
	// directory and retry only these errors, with a bounded wait for real failures.
	started := time.Now()
	delay := time.Millisecond

	for {
		_, destinationError := os.Lstat(destination)
		if !errors.Is(destinationError, os.ErrNotExist) {
			return err
		}

		remaining := publishRetryTimeout - time.Since(started)
		if remaining <= 0 {
			return err
		}

		time.Sleep(min(delay, remaining))

		err = os.Rename(staging, destination)
		if !retryablePublishError(err) {
			return err
		}

		delay = min(delay*2, 100*time.Millisecond)
	}
}

func retryablePublishError(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errorSharingViolation)
}
