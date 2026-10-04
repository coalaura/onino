package onion

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

var benchmarkHostname string

func BenchmarkHitCosts(b *testing.B) {
	key := testKey()

	b.Run("validate", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			err := validateKey(&key)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("hostname", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			benchmarkHostname = key.Hostname()
		}
	})

	// Each timed save retains validation, all three file Sync calls, and rename.
	// Unique keys and cleanup are outside the timer; memory and disk stay bounded.
	b.Run("store", func(b *testing.B) {
		directory := b.TempDir()

		store, err := NewStore(directory)
		if err != nil {
			b.Fatal(err)
		}

		counter := uint64(0)

		b.ReportAllocs()

		for b.Loop() {
			b.StopTimer()

			counter++

			var seed [32]byte

			binary.LittleEndian.PutUint64(seed[:], counter)

			private := ed25519.NewKeyFromSeed(seed[:])

			key := Key{Secret: sha512.Sum512(seed[:])}

			key.Secret[0] &= 248
			key.Secret[31] &= 63
			key.Secret[31] |= 64

			copy(key.Public[:], private[32:])

			b.StartTimer()

			err = store.Save(key)
			if err != nil {
				b.Fatal(err)
			}

			b.StopTimer()

			err = os.RemoveAll(filepath.Join(directory, key.Hostname()))
			if err != nil {
				b.Fatal(err)
			}

			b.StartTimer()
		}
	})
}
