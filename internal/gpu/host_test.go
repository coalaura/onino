//go:build gpu

package gpu

import (
	"encoding/base32"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestPrefixMasks(t *testing.T) {
	var public [32]byte

	for index := range public {
		public[index] = byte(index*71 + 3)
	}

	encoded := strings.ToLower(base32.StdEncoding.EncodeToString(public[:]))
	word := binary.LittleEndian.Uint64(public[:8])

	for length := 1; length <= 51; length++ {
		plan, err := Compile([]string{encoded[:length] + "."})
		if err != nil {
			t.Fatal(err)
		}

		probe := plan.probes[0]

		mask := uint64(probe[0]) | uint64(probe[1])<<32
		value := uint64(probe[2]) | uint64(probe[3])<<32

		if word&mask != value {
			t.Fatalf("length %d rejects a matching public key", length)
		}

		for bit := range 64 {
			byteBit := (bit/8)*8 + 7 - bit%8

			expected := byteBit < min(length, 12)*5
			if (mask&(uint64(1)<<bit) != 0) != expected {
				t.Fatalf("length %d bit %d: wrong mask %016x", length, bit, mask)
			}
		}
	}

	invalid := [][]string{nil, {strings.Repeat("a", 52) + "."}, {"a.", "b.", "c.", "d.", "e.", "f.", "g.", "h.", "i."}}

	for _, patterns := range invalid {
		_, err := Compile(patterns)
		if err == nil {
			t.Fatalf("accepted invalid plan %v", patterns)
		}
	}
}

func TestVerifierRejectsCorruptionAndReplay(t *testing.T) {
	state, _, err := newSeed(1)
	if err != nil {
		t.Fatal(err)
	}

	key, err := reconstruct(state.secret, 65)
	if err != nil {
		t.Fatal(err)
	}

	prefix := key.Hostname()[:12] + "."

	matcher, err := pattern.CompilePatterns([]string{prefix})
	if err != nil {
		t.Fatal(err)
	}

	var saves int

	worker := verifier{seeds: []seed{state}, matcher: matcher}

	worker.save = func(actual onion.Key, found time.Time) error {
		saves++

		if actual != key || found.IsZero() {
			t.Error("verifier changed the reconstructed key or timestamp")
		}

		return nil
	}

	valid := hit{Generation: 1, Steps: 65, Low: binary.LittleEndian.Uint32(key.Public[:4]), High: binary.LittleEndian.Uint32(key.Public[4:8])}

	corrupt := []hit{valid, valid, valid, valid}
	corrupt[0].Stream = ^uint32(0)
	corrupt[1].Generation = 0
	corrupt[2].Kind = 2
	corrupt[3].Low ^= 1

	for _, result := range corrupt {
		_, err = worker.verify(discovered{hit: result, found: time.Now()})
		if err == nil || saves != 0 || worker.saved.Load() != 0 {
			t.Fatalf("accepted corrupt hit %+v", result)
		}
	}

	reply, err := worker.verify(discovered{hit: valid, found: time.Now()})
	if err != nil || saves != 1 || worker.saved.Load() != 1 || reply.command.Generation != 2 {
		t.Fatalf("valid hit: %+v, saves=%d, error=%v", reply, saves, err)
	}

	_, err = worker.verify(discovered{hit: valid, found: time.Now()})
	if err == nil || saves != 1 {
		t.Fatal("replayed generation exported a second key")
	}

	worker.seeds[0] = state

	other := "a."

	if prefix[0] == 'a' {
		other = "b."
	}

	worker.matcher, err = pattern.CompilePatterns([]string{other})
	if err != nil {
		t.Fatal(err)
	}

	reply, err = worker.verify(discovered{hit: valid, found: time.Now()})
	if err != nil || saves != 1 || reply.command.Action != 1 || reply.command.Expected != 1 || worker.seeds[0] != state {
		t.Fatalf("false positive did not preserve its seed: %+v, %v", reply, err)
	}
}
