package pattern

// PrefixProbe is a necessary condition on the canonical little-endian first word.
type PrefixProbe struct {
	Mask  uint64
	Value uint64
}

// PrefixPlan is a startup-only description for vector candidate filtering.
// Survivors still require sign recovery and ordinary exact matching.
type PrefixPlan struct {
	Probes [8]PrefixProbe
	Count  uint64
}

func (matcher *Matcher) PrefixPlan() PrefixPlan {
	var plan PrefixPlan

	filter := matcher.SignFilter()
	if filter == nil || filter.offset != 0 {
		return plan
	}

	switch filter.kind {
	case matcherSingle, matcherOneWide:
		plan.Probes[0] = PrefixProbe{Mask: filter.single.mask, Value: filter.single.value}
		plan.Count = 1
	case matcherSingleWordSet:
		probes := filter.tables[0].probes
		if len(probes) > len(plan.Probes) {
			return plan
		}

		for index := range probes {
			plan.Probes[index] = PrefixProbe{Mask: probes[index].mask, Value: probes[index].value}
		}

		plan.Count = uint64(len(probes))
	}

	return plan
}
