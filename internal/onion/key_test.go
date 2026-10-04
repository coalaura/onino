package onion

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHostnameSpecVectors(t *testing.T) {
	addresses := []string{
		"pg6mmjiyjmcrsslvykfwnntlaru7p5svn6y2ymmju6nubxndf4pscryd.onion",
		"sp3k262uwy4r2k3ycr5awluarykdpag6a7y33jxop4cs2lu5uz5sseqd.onion",
		"xa4r2iadxm55fbnqgwwi5mymqdcofiu3w6rpbtqn7b2dyn7mgwj64jyd.onion",
	}

	for _, address := range addresses {
		decoded, err := encoding.DecodeString(address[:56])
		if err != nil {
			t.Fatal(err)
		}

		var key Key

		copy(key.Public[:], decoded[:32])

		actual := key.Hostname()
		if actual != address {
			t.Fatalf("got %s, want specification vector %s", actual, address)
		}
	}
}

func TestStoreTorFiles(t *testing.T) {
	key := testKey()
	directory := t.TempDir()

	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}

	err = store.Save(key)
	if err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(directory, key.Hostname())

	secret, err := os.ReadFile(filepath.Join(destination, "hs_ed25519_secret_key"))
	if err != nil {
		t.Fatal(err)
	}

	public, err := os.ReadFile(filepath.Join(destination, "hs_ed25519_public_key"))
	if err != nil {
		t.Fatal(err)
	}

	hostname, err := os.ReadFile(filepath.Join(destination, "hostname"))
	if err != nil {
		t.Fatal(err)
	}

	if len(secret) != 96 || string(secret[:32]) != "== ed25519v1-secret: type0 ==\x00\x00\x00" || !bytes.Equal(secret[32:], key.Secret[:]) {
		t.Fatal("incorrect Tor secret key file")
	}

	if len(public) != 64 || string(public[:32]) != "== ed25519v1-public: type0 ==\x00\x00\x00" || !bytes.Equal(public[32:], key.Public[:]) {
		t.Fatal("incorrect Tor public key file")
	}

	if string(hostname) != key.Hostname()+"\n" {
		t.Fatal("incorrect hostname file")
	}

	// POSIX permission bits are not the Windows ACL representation.
	if runtime.GOOS != "windows" {
		checkMode(t, destination, 0o700)
		checkMode(t, filepath.Join(destination, "hs_ed25519_secret_key"), 0o600)
	}

	err = store.Save(key)
	if err == nil {
		t.Fatal("existing match was overwritten")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".onino-") {
		t.Fatal("successful save left staging files")
	}
}

func TestStoreRejectsInvalidKey(t *testing.T) {
	directory := t.TempDir()

	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}

	key := testKey()

	key.Public[0] ^= 1

	err = store.Save(key)
	if err == nil {
		t.Fatal("mismatched public and secret key accepted")
	}

	key = testKey()

	key.Secret[0] |= 1

	err = store.Save(key)
	if err == nil {
		t.Fatal("unclamped scalar accepted")
	}

	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid keys created files: %v", err)
	}
}

func testKey() Key {
	seed := [32]byte{1, 2, 3, 4}

	private := ed25519.NewKeyFromSeed(seed[:])

	key := Key{Secret: sha512.Sum512(seed[:])}

	key.Secret[0] &= 248
	key.Secret[31] &= 63
	key.Secret[31] |= 64

	copy(key.Public[:], private[32:])

	return key
}

func checkMode(t *testing.T, filename string, expected os.FileMode) {
	t.Helper()

	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != expected {
		t.Fatalf("%s permissions: %o, want %o", filename, info.Mode().Perm(), expected)
	}
}
