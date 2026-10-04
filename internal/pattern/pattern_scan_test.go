package pattern

import (
	"testing"
	"unsafe"
)

type layoutCheck struct {
	name string
	got  uintptr
	want uintptr
}

func TestScanLayout(t *testing.T) {
	var plan scanPlan

	checks := []layoutCheck{
		{name: "positions offset", got: unsafe.Offsetof(plan.positions), want: 0},
		{name: "mask offset", got: unsafe.Offsetof(plan.mask), want: 8},
		{name: "value offset", got: unsafe.Offsetof(plan.value), want: 12},
		{name: "plan stride", got: unsafe.Sizeof(plan), want: 16},
		{name: "word probe size", got: unsafe.Sizeof(wordProbe{}), want: 16},
		{name: "verification size", got: unsafe.Sizeof(bitPattern{}), want: 64},
	}

	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
}
