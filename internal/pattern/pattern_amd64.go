package pattern

var vectorAvailable = supportsAVX2()

//go:noescape
func supportsAVX2() bool

//go:noescape
//go:abiinternal data=AX plans=BX count=CX -> index=AX candidates=BX
func filterCandidates(data *[32]byte, plans *scanPlan, count int) (index int, candidates uint64)
