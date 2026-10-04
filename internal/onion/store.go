package onion

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/edwards25519"
)

const (
	secretHeader = "== ed25519v1-secret: type0 ==\x00\x00\x00"
	publicHeader = "== ed25519v1-public: type0 ==\x00\x00\x00"
)

// Store saves each match in its own Tor-compatible service directory.
type Store struct {
	directory string
}

// Save validates the key, flushes all three files in a private staging
// directory, then renames it to the hostname. Existing matches are not replaced.
// On failure the staging directory is retained and its path is in the error.
func (store *Store) Save(key Key) error {
	err := validateKey(&key)
	if err != nil {
		return err
	}

	hostname := key.Hostname()
	destination := filepath.Join(store.directory, hostname)

	_, err = os.Lstat(destination)
	if err == nil {
		return fmt.Errorf("match already exists: %s", destination)
	}

	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	staging, err := os.MkdirTemp(store.directory, ".onino-")
	if err != nil {
		return err
	}

	err = writeKeyFiles(staging, &key, hostname)
	if err != nil {
		return fmt.Errorf("save match in %s: %w", staging, err)
	}

	err = os.Rename(staging, destination)
	if err != nil {
		return fmt.Errorf("publish match from %s: %w", staging, err)
	}

	return nil
}

func NewStore(directory string) (*Store, error) {
	err := os.MkdirAll(directory, 0o700)
	if err != nil {
		return nil, err
	}

	return &Store{directory: directory}, nil
}

func validateKey(key *Key) error {
	if key.Secret[0]&7 != 0 || key.Secret[31]&192 != 64 {
		return errors.New("invalid expanded Ed25519 scalar")
	}

	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(key.Secret[:32])
	if err != nil {
		return err
	}

	public := new(edwards25519.Point).ScalarBaseMult(scalar)
	if !bytes.Equal(public.Bytes(), key.Public[:]) {
		return errors.New("expanded secret does not match public key")
	}

	return nil
}

func writeKeyFiles(directory string, key *Key, hostname string) error {
	var secretFile [96]byte

	copy(secretFile[:32], secretHeader)
	copy(secretFile[32:], key.Secret[:])

	err := writeSynced(filepath.Join(directory, "hs_ed25519_secret_key"), secretFile[:])
	if err != nil {
		return err
	}

	var publicFile [64]byte

	copy(publicFile[:32], publicHeader)
	copy(publicFile[32:], key.Public[:])

	err = writeSynced(filepath.Join(directory, "hs_ed25519_public_key"), publicFile[:])
	if err != nil {
		return err
	}

	return writeSynced(filepath.Join(directory, "hostname"), []byte(hostname+"\n"))
}

func writeSynced(filename string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	_, writeError := file.Write(data)
	if writeError == nil {
		writeError = file.Sync()
	}

	closeError := file.Close()

	return errors.Join(writeError, closeError)
}
