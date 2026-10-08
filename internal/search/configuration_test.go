package search

import (
	"context"
	"crypto/sha3"
	"errors"
	"strings"
	"testing"

	"filippo.io/edwards25519"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

type configurationCase struct {
	name     string
	features simd.Features
	scalar   bool
	vector   bool
	want     simd.Mode
}

func TestAutomaticConfiguration(t *testing.T) {
	matcher, err := pattern.CompilePatterns([]string{"somethingrare."})
	if err != nil {
		t.Fatal(err)
	}

	cases := []configurationCase{
		{name: "portable", want: simd.Portable},
		{name: "no features", scalar: true, vector: true, want: simd.Portable},
		{name: "BMI2 without vector state", features: simd.Features{BMI2: true, AVX2CPU: true, IFMACPU: true}, scalar: true, vector: true, want: simd.BMI2},
		{name: "ADX alone", features: simd.Features{ADX: true}, scalar: true, want: simd.Portable},
		{name: "BMI2 ADX", features: simd.Features{BMI2: true, ADX: true}, scalar: true, want: simd.BMI2ADX},
		{name: "IFMA", features: simd.Features{BMI2: true, ADX: true, IFMA: true, VL: true}, scalar: true, vector: true, want: simd.IFMA},
		{name: "uncompiled", features: simd.Features{BMI2: true, ADX: true, IFMA: true, VL: true}, want: simd.Portable},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config, resolveErr := resolveConfiguration(simd.Auto, matcher, test.features, test.scalar, test.vector)
			if resolveErr != nil || config.Engine != test.want || config.Forced() || config.Features != test.features {
				t.Fatalf("configuration: %+v, %v", config, resolveErr)
			}
		})
	}
}

func TestUnconditionalConfiguration(t *testing.T) {
	matcher, err := pattern.CompilePatterns([]string{"bc"})
	if err != nil {
		t.Fatal(err)
	}

	modes := []simd.Mode{simd.Portable, simd.BMI2, simd.BMI2ADX, simd.IFMA}
	missing := simd.Features{Known: true}

	for _, mode := range modes {
		config, resolveErr := resolveConfiguration(mode, matcher, missing, true, true)
		if resolveErr != nil || config.Engine != mode || !config.Forced() || config.Features != missing {
			t.Fatalf("forced %s: %+v, %v", mode, config, resolveErr)
		}

		if mode != simd.Portable && config.Supported() {
			t.Fatalf("forced %s falsely reported supported", mode)
		}

		if mode == simd.IFMA && config.Independent {
			t.Fatal("performance preference overrode forced IFMA")
		}

		_, resolveErr = resolveConfiguration(mode, matcher, missing, false, false)
		if mode == simd.Portable && resolveErr != nil {
			t.Fatal(resolveErr)
		}

		if mode != simd.Portable && (resolveErr == nil || !strings.Contains(resolveErr.Error(), "not compiled")) {
			t.Fatalf("absent %s: %v", mode, resolveErr)
		}
	}
}

func TestForcedSearchExecution(t *testing.T) {
	workloads := [][]string{{"somethingrare."}, {"bc"}, {".aaaa"}}
	modes := executableModes()

	for _, mode := range modes {
		for _, patterns := range workloads {
			t.Run(mode.String()+"/"+patterns[0], func(t *testing.T) {
				matcher, err := pattern.CompilePatterns(patterns)
				if err != nil {
					t.Fatal(err)
				}

				// Real hardware gates this test only; execution sees no reported features.
				config, err := resolveConfiguration(mode, matcher, simd.Features{Known: true}, scalarAssemblyAvailable, vectorAssemblyAvailable)
				if err != nil {
					t.Fatal(err)
				}

				state, err := newWorkerWithConfig(sha3.NewSHAKE256(), matcher, config)
				if err != nil {
					t.Fatal(err)
				}

				checkWorkerMode(t, state, config)

				var stats Stats

				for range pairedOffsets + 2 {
					err = state.searchBatch(matcher, func(key onion.Key) error {
						checkKey(t, &key)
						checkPairedKey(t, key)

						if !matcher.Match(key.Public) {
							t.Fatal("saved a nonmatch")
						}

						return nil
					}, &stats)

					if err != nil {
						t.Fatal(err)
					}
				}

				if stats.Checked != (pairedOffsets+2)*batchSize {
					t.Fatalf("candidate accounting: %+v", stats)
				}

				checkWorkerMode(t, state, config)
			})
		}
	}
}

func TestForcedCancellationAndIndependence(t *testing.T) {
	matcher, err := pattern.CompilePatterns(allSuffixPatterns(1))
	if err != nil {
		t.Fatal(err)
	}

	for _, mode := range executableModes() {
		t.Run(mode.String(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			keys := make([]onion.Key, 0, batchSize)
			nonces := make(map[[32]byte]bool, batchSize)

			stats, runErr := runWithSIMD(ctx, matcher, func(key onion.Key) error {
				checkKey(t, &key)

				nonce := [32]byte(key.Secret[32:])
				if nonces[nonce] {
					t.Fatal("saved two keys from one seed")
				}

				nonces[nonce] = true
				keys = append(keys, key)
				cancel()

				return nil
			}, nil, mode)

			if !errors.Is(runErr, context.Canceled) || stats.Checked != batchSize || stats.Saved != batchSize || len(keys) != batchSize {
				t.Fatalf("cancellation accounting: %+v, %v", stats, runErr)
			}

			for _, key := range keys {
				checkPairedKey(t, key)
			}
		})
	}
}

func TestScalarBackendKeys(t *testing.T) {
	for _, mode := range executableModes() {
		if mode == simd.IFMA {
			continue
		}

		t.Run(mode.String(), func(t *testing.T) {
			state := &pairedGenerator{random: sha3.NewSHAKE256(), fieldMode: mode}

			err := state.reset()
			if err != nil {
				t.Fatal(err)
			}

			original := state.secrets[3]
			state.steps[3] = reseedRounds - pairedOffsets
			centerSecret := offsetSecret(original, state.steps[3])

			scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(centerSecret[:32])
			if err != nil {
				t.Fatal(err)
			}

			state.centers[3].set(new(edwards25519.Point).ScalarBaseMult(scalar))

			for range pairedOffsets + 2 {
				err = state.nextBatch()
				if err != nil {
					t.Fatal(err)
				}

				for index := range state.publicKeys {
					state.completeSign(index)
					checkPairedKey(t, state.key(index))
				}
			}

			if state.secrets[3] == original || state.fieldMode != mode {
				t.Fatal("reseed did not preserve the selected backend")
			}
		})
	}
}

func executableModes() []simd.Mode {
	modes := make([]simd.Mode, 0, 4)
	modes = append(modes, simd.Portable)

	features := simd.Detect()

	if scalarAssemblyAvailable && features.BMI2 {
		modes = append(modes, simd.BMI2)

		if features.ADX {
			modes = append(modes, simd.BMI2ADX)
		}
	}

	if vectorAssemblyAvailable && features.IFMA && (!ifmaNeedsVL || features.VL) {
		modes = append(modes, simd.IFMA)
	}

	return modes
}
