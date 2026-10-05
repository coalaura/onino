package cpu

import (
	"errors"
	"testing"
)

type resolveCase struct {
	value string
	want  int
}

func TestResolve(t *testing.T) {
	cases := []resolveCase{{"1", 1}, {"32", 32}, {"all", 32}, {"01", 1}, {"", 0}, {"0", 0}, {"-1", 0}, {"+1", 0}, {"1.5", 0}, {"ALL", 0}, {" 2", 0}, {"2 ", 0}, {"33", 0}, {"99999999999999999999999999999", 0}}

	for _, entry := range cases {
		got, err := Resolve(entry.value, 32)
		if got != entry.want || (err != nil) != (entry.want == 0) {
			t.Errorf("Resolve(%q): %d, %v; want %d", entry.value, got, err, entry.want)
		}
	}

	_, err := Resolve("all", 0)
	if err == nil {
		t.Fatal("accepted empty CPU availability")
	}
}

func TestSelectTopology(t *testing.T) {
	// Deliberately irregular numbering, sibling order and processor groups.
	topology := Topology{
		Known: true,
		CPUs: []CPU{
			{Group: 0, Number: 9, Core: 3, Cache: 7},
			{Group: 0, Number: 1, Core: 3, Cache: 7},
			{Group: 0, Number: 6, Core: 2, Cache: 7},
			{Group: 1, Number: 4, Core: 9, Cache: 8},
			{Group: 1, Number: 0, Core: 8, Cache: 8},
			{Group: 1, Number: 2, Core: 8, Cache: 8},
		},
	}

	wantPacked := []int{9, 6, 4, 0, 1, 2}
	wantSpread := []int{9, 4, 6, 0, 1, 2}
	placements := []bool{false, true}

	for _, spread := range placements {
		want := wantPacked

		if spread {
			want = wantSpread
		}

		for count := 1; count <= len(topology.CPUs); count++ {
			got, err := Select(topology, count, spread)
			if err != nil {
				t.Fatal(err)
			}

			for index, processor := range got {
				if processor.Number != want[index] {
					t.Fatalf("spread=%v count=%d: %+v", spread, count, got)
				}
			}
		}
	}

	_, err := Select(topology, 7, false)
	if err == nil {
		t.Fatal("accepted excessive placement")
	}

	topology.Known = false

	_, err = Select(topology, 1, false)
	if err == nil {
		t.Fatal("invented physical topology")
	}
}

func TestDiscover(t *testing.T) {
	topology, err := Discover()
	if err != nil {
		t.Fatal(err)
	}

	if len(topology.CPUs) == 0 {
		t.Fatal("no available processors")
	}

	t.Logf("known=%v CPUs=%+v", topology.Known, topology.CPUs)
}

func TestAffinityRetirement(t *testing.T) {
	failure := errors.New("cannot restore affinity")

	var unlocked bool

	unlock := func() {
		unlocked = true
	}

	state := threadAffinity{
		restore: func() error {
			return failure
		},
	}

	err := state.release(unlock)
	if !errors.Is(err, failure) || unlocked {
		t.Fatal("restoration failure returned a constrained thread to the runtime")
	}

	state.restore = func() error {
		return nil
	}

	state.retire = true

	err = state.release(unlock)
	if err != nil || unlocked {
		t.Fatal("ambiguous affinity was not retired")
	}

	state.retire = false

	err = state.release(unlock)
	if err != nil || !unlocked {
		t.Fatal("restored thread was not unlocked")
	}
}
