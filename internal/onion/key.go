// Package onion encodes v3 onion addresses and Tor's expanded Ed25519 key files.
package onion

import (
	"crypto/sha3"
	"encoding/base32"
)

const (
	checksumPrefix = ".onion checksum"
	version        = 3
)

// Key contains a public key and an expanded Ed25519 secret: a clamped scalar
// followed by its 32-byte nonce prefix. Secret is not a Go ed25519.PrivateKey.
type Key struct {
	Public [32]byte
	Secret [64]byte
}

var encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// Hostname returns the full, checksummed 56-character label plus ".onion".
func (key *Key) Hostname() string {
	var checksumInput [len(checksumPrefix) + 32 + 1]byte

	copy(checksumInput[:], checksumPrefix)
	copy(checksumInput[len(checksumPrefix):], key.Public[:])

	checksumInput[len(checksumInput)-1] = version

	checksum := sha3.Sum256(checksumInput[:])

	var address [35]byte

	copy(address[:], key.Public[:])
	copy(address[32:], checksum[:2])

	address[34] = version

	var hostname [62]byte

	encoding.Encode(hostname[:56], address[:])

	copy(hostname[56:], ".onion")

	return string(hostname[:])
}
