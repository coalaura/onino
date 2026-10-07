//go:build gpu

package gpu

import (
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"math/big"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

var (
	//go:embed shaders/arithmetic.spv
	arithmeticShader []byte

	//go:embed shaders/diagnostic.spv
	diagnosticShader []byte

	//go:embed shaders/epoch.spv
	epochShader []byte
)

func TestCandidateRanges(t *testing.T) {
	requireVulkan(t)

	// A zero-mask probe exposes every candidate, including candidates the production prefix filter rejects.
	plan := Plan{count: 1}

	engine, err := openDevice(testDevice(t), true, 1, 2, makeTable(plan), diagnosticShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	state, initial, err := newSeed(1)
	if err != nil {
		t.Fatal(err)
	}

	commands := []command{initial}

	for candidate := range 260 {
		err = engine.submit(0, commands, 2, false)
		if err != nil {
			t.Fatal(err)
		}

		completed, collectError := engine.collect(0)
		if collectError != nil {
			t.Fatal(collectError)
		}

		if len(completed.hits) != 2 || completed.checked != 1 {
			t.Fatalf("candidate %d: hits=%d checked=%d", candidate, len(completed.hits), completed.checked)
		}

		result := completed.hits[0]
		center := uint32(64 + 129*(candidate/128))
		position := uint32((candidate%128)/2 + 1)
		steps := center + position

		if candidate%2 != 0 {
			steps = center - position
		}

		if result.Stream != steps || result.Generation != 1 {
			t.Fatalf("candidate %d: unexpected record %+v, expected steps %d", candidate, result, steps)
		}

		key, reconstructError := reconstruct(state.secret, steps)
		if reconstructError != nil {
			t.Fatal(reconstructError)
		}

		second := completed.hits[1]
		limbs := []uint32{result.Steps, result.Kind, result.Low, result.High, second.Stream, second.Generation, second.Steps, second.Kind, second.Low, second.High & 0x1ffffff}

		actual := encodeLimbs(limbs)
		actual[31] |= byte(second.High >> 25 << 7)

		if actual != key.Public {
			t.Fatalf("candidate %d steps %d: GPU %x CPU %x", candidate, steps, actual, key.Public)
		}

		commands[0] = command{Expected: 1, Action: 1}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func TestArithmetic(t *testing.T) {
	requireVulkan(t)

	commands := make([]command, 320)
	expected := make([][32]byte, len(commands))

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

	random := rand.New(rand.NewPCG(1, 2))

	for index := range commands {
		var (
			first  [32]byte
			second [32]byte
		)

		for word := range 4 {
			binary.LittleEndian.PutUint64(first[word*8:], random.Uint64())
			binary.LittleEndian.PutUint64(second[word*8:], random.Uint64())
		}

		first[31] &= 127
		second[31] &= 127

		if index < 25 {
			value := new(big.Int).Add(prime, big.NewInt(int64(index/5)-2))
			first = littleInteger(value)
		}

		if index >= 25 && index < 30 {
			first = [32]byte{}
		}

		putLimbs(commands[index].Center[:10], first[:])
		putLimbs(commands[index].Center[10:20], second[:])

		commands[index].Action = uint32(index % 5)

		left := bigInteger(first)
		right := bigInteger(second)
		result := new(big.Int)

		switch index % 5 {
		case 0:
			result.Add(left, right)
		case 1:
			result.Sub(left, right)
		case 2:
			result.Mul(left, right)
		case 3:
			result.Mul(left, left)
		case 4:
			result.Exp(left, new(big.Int).Sub(prime, big.NewInt(2)), prime)
		}

		result.Mod(result, prime)
		expected[index] = littleInteger(result)
	}

	engine, err := openDevice(testDevice(t), true, len(commands), len(commands)*2, makeTable(Plan{}), arithmeticShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	err = engine.submit(0, commands, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	completed, err := engine.collect(0)
	if err != nil {
		t.Fatal(err)
	}

	for index := range commands {
		first := completed.hits[2*index]
		second := completed.hits[2*index+1]

		limbs := []uint32{first.Stream, first.Generation, first.Steps, first.Kind, first.Low, first.High, second.Stream, second.Generation, second.Steps, second.Kind}

		actual := encodeLimbs(limbs)
		if actual != expected[index] {
			t.Fatalf("operation %d case %d: GPU %x CPU %x", index%5, index, actual, expected[index])
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func TestOverflowPauseAndGeneration(t *testing.T) {
	requireVulkan(t)

	engine, err := openDevice(testDevice(t), true, 5, 1, makeTable(Plan{count: 1}), searchShader)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("initialized device: %s", engine.name)

	defer engine.close()

	commands := make([]command, 5)

	for index := range commands {
		_, commands[index], err = newSeed(1)
		if err != nil {
			t.Fatal(err)
		}
	}

	seen := make(map[uint32]bool, 5)

	for iteration := range 6 {
		completed := dispatch(t, engine, commands, iteration > 0)

		clear(commands)

		if iteration == 0 && completed.checked != 5 {
			t.Fatalf("initial checked=%d", completed.checked)
		}

		if iteration > 0 && completed.checked != 0 {
			t.Fatal("paused stream searched new candidates")
		}

		for _, result := range completed.hits {
			if seen[result.Stream] {
				t.Fatal("duplicate delivery")
			}

			seen[result.Stream] = true
		}

		if completed.pending != uint32(max(4-iteration, 0)) {
			t.Fatalf("unexpected pending count %d", completed.pending)
		}
	}

	if len(seen) != 5 {
		t.Fatal("overflow lost a hit")
	}

	_, commands[0], err = newSeed(2)
	if err != nil {
		t.Fatal(err)
	}

	completed := dispatch(t, engine, commands, false)
	if len(completed.hits) != 1 || completed.hits[0].Generation != 2 {
		t.Fatal("replacement generation did not start")
	}

	clear(commands)

	commands[0] = command{Expected: 1, Action: 1}

	completed = dispatch(t, engine, commands, false)
	if len(completed.hits) != 0 || completed.checked != 0 {
		t.Fatal("stale acknowledgement resumed a stream")
	}

	commands[0] = command{Expected: 2, Action: 1}

	completed = dispatch(t, engine, commands, false)
	if len(completed.hits) != 1 || completed.hits[0].Steps != 63 {
		t.Fatal("resume lost the paired sibling")
	}

	clear(commands)

	allocations := testing.AllocsPerRun(20, func() {
		dispatch(t, engine, commands, true)
	})

	if allocations != 0 {
		t.Fatalf("steady-state submission allocated %g Go objects", allocations)
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func TestCancellationAndSavedKeys(t *testing.T) {
	requireVulkan(t)

	matcher, err := pattern.CompilePatterns([]string{"a."})
	if err != nil {
		t.Fatal(err)
	}

	store, err := onion.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keys := make(map[[32]byte]bool)

	stats, _, err := Run(ctx, Plan{count: 1}, matcher, func(key onion.Key, found time.Time) error {
		if keys[key.Public] || !matcher.Match(key.Public) || found.IsZero() {
			t.Error("invalid or duplicate exported key")
		}

		keys[key.Public] = true

		cancel()

		return store.Save(key)
	}, Options{Device: testDevice(t), Validation: true, Streams: 8, Capacity: 1})

	if !errors.Is(err, context.Canceled) || stats.Saved != uint64(len(keys)) || stats.Saved == 0 {
		t.Fatalf("cancellation stats %+v: %v", stats, err)
	}

	saveError := errors.New("save failed")

	stats, _, err = Run(context.Background(), Plan{count: 1}, matcher, func(key onion.Key, found time.Time) error {
		return saveError
	}, Options{Device: testDevice(t), Validation: true, Streams: 8, Capacity: 1})

	if !errors.Is(err, saveError) || stats.Saved != 0 {
		t.Fatalf("save failure stats %+v: %v", stats, err)
	}
}

func TestCancellationDrainsOverflow(t *testing.T) {
	requireVulkan(t)

	patterns := make([]string, 0, 32)

	for _, character := range alphabet {
		patterns = append(patterns, string(character)+".")
	}

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	settings := []Options{
		{Device: testDevice(t), Validation: true, Streams: 8, Capacity: 1},
		{Device: testDevice(t), Validation: true, Streams: 4096, Rounds: 2},
	}

	for _, options := range settings {
		t.Run(strconv.Itoa(options.Streams), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			monitor := search.NewMonitor()
			options.Monitor = monitor

			readyCount := 0

			options.Ready = func(device string) {
				readyCount++

				if device == "" || monitor.Snapshot() != (search.Stats{}) {
					t.Error("startup did not precede completed work on a selected device")
				}
			}

			stats, _, runError := Run(ctx, Plan{count: 1}, matcher, func(key onion.Key, found time.Time) error {
				live := monitor.Snapshot()
				if live.Checked != uint64(options.Streams) || live.Saved >= live.Checked {
					t.Errorf("snapshot counts uncompleted work or unsaved matches: %+v", live)
				}

				cancel()

				return nil
			}, options)

			if !errors.Is(runError, context.Canceled) || stats.Checked != uint64(options.Streams) || stats.Saved != stats.Checked {
				t.Fatalf("lost pending matches on cancellation: %+v, %v", stats, runError)
			}

			if readyCount != 1 || monitor.Snapshot() != stats {
				t.Fatal("missing startup notification or completed dispatches/drained saves")
			}

			select {
			case <-monitor.Done():
			default:
				t.Fatal("GPU shutdown did not stop reporting")
			}
		})
	}
}

func TestPrefixFiltering(t *testing.T) {
	requireVulkan(t)

	patterns := []string{"a.", "b.", "c."}

	plan, err := Compile(patterns)
	if err != nil {
		t.Fatal(err)
	}

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	state, initial, err := newSeed(1)
	if err != nil {
		t.Fatal(err)
	}

	engine, err := openDevice(testDevice(t), true, 1, 1, makeTable(plan), searchShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	commands := []command{initial}
	checked := uint32(0)

	for candidate := range 384 {
		center := uint32(64 + 129*(candidate/128))
		position := uint32(candidate%128/2 + 1)

		steps := center + position

		if candidate%2 != 0 {
			steps = center - position
		}

		key, reconstructError := reconstruct(state.secret, steps)
		if reconstructError != nil {
			t.Fatal(reconstructError)
		}

		if !matcher.Match(key.Public) {
			continue
		}

		for {
			completed := dispatch(t, engine, commands, false)
			checked += completed.checked

			clear(commands)

			if len(completed.hits) == 0 {
				if checked > uint32(candidate) {
					t.Fatal("GPU rejected a CPU match")
				}

				continue
			}

			result := completed.hits[0]
			if checked != uint32(candidate+1) || result.Steps != steps || uint64(result.Low)|uint64(result.High)<<32 != binary.LittleEndian.Uint64(key.Public[:8]) {
				t.Fatalf("filter diverged at candidate %d: checked=%d record=%+v", candidate, checked, result)
			}

			commands[0] = command{Expected: 1, Action: 1}

			break
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func TestCompile(t *testing.T) {
	rejected := []string{"", ".abc", "a.b", ".abc.", "abc", "ABC.", "abc9."}

	for _, pattern := range rejected {
		_, err := Compile([]string{pattern})
		if err == nil {
			t.Fatalf("accepted unsupported GPU pattern %q", pattern)
		}
	}
}

func TestEpochBoundary(t *testing.T) {
	requireVulkan(t)

	// This variant starts at the last range. No probe can interrupt exhaustion.
	engine, err := openDevice(testDevice(t), true, 1, 1, makeTable(Plan{}), epochShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	_, initial, err := newSeed(1)
	if err != nil {
		t.Fatal(err)
	}

	commands := []command{initial}
	completed := dispatch(t, engine, commands, false)

	if completed.checked != 128 || len(completed.hits) != 1 || completed.hits[0].Kind != 1 || completed.hits[0].Generation != 1 {
		t.Fatalf("range exhaustion: %+v", completed)
	}

	clear(commands)

	completed = dispatch(t, engine, commands, false)

	if completed.checked != 0 || len(completed.hits) != 0 {
		t.Fatal("exhausted stream continued searching")
	}

	_, commands[0], err = newSeed(2)
	if err != nil {
		t.Fatal(err)
	}

	completed = dispatch(t, engine, commands, false)

	if completed.checked != 128 || len(completed.hits) != 1 || completed.hits[0].Generation != 2 {
		t.Fatal("exhausted stream did not accept replacement")
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func requireVulkan(t *testing.T) {
	t.Helper()

	if os.Getenv("ONINO_VULKAN_TEST") != "1" {
		t.Skip("set ONINO_VULKAN_TEST=1 and expose the Khronos validation layer to run device correctness checks")
	}
}

func testDevice(t *testing.T) int {
	t.Helper()

	value := os.Getenv("ONINO_VULKAN_DEVICE")
	if value == "" {
		return 0
	}

	index, err := strconv.Atoi(value)
	if err != nil || index < 0 {
		t.Fatal("ONINO_VULKAN_DEVICE must be a nonnegative device index")
	}

	return index
}

func dispatch(t *testing.T, engine *device, commands []command, collectOnly bool) collection {
	t.Helper()

	err := engine.submit(0, commands, 2, collectOnly)
	if err != nil {
		t.Fatal(err)
	}

	completed, err := engine.collect(0)
	if err != nil {
		t.Fatal(err)
	}

	return completed
}

func encodeLimbs(limbs []uint32) [32]byte {
	var encoded [32]byte

	position := 0

	for index, limb := range limbs {
		for bit := range 26 - index%2 {
			encoded[position/8] |= byte((limb>>bit)&1) << (position % 8)
			position++
		}
	}

	return encoded
}

func bigInteger(encoded [32]byte) *big.Int {
	for index := range 16 {
		encoded[index], encoded[31-index] = encoded[31-index], encoded[index]
	}

	return new(big.Int).SetBytes(encoded[:])
}

func littleInteger(value *big.Int) [32]byte {
	var encoded [32]byte

	value.FillBytes(encoded[:])

	for index := range 16 {
		encoded[index], encoded[31-index] = encoded[31-index], encoded[index]
	}

	return encoded
}
