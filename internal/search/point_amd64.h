// Complete mixed addition: seven multiplications, with caller-owned scratch.
// Point, scratch and the read-only step occupy disjoint storage. Every scratch
// slot is written before being read; scratch is reused between independent lanes.
// X1-X3 keep pointers live across PRODUCT's register clobbers. No Go pointer is
// stored outside the caller's stack; there are no calls or safe points here.

TEXT ADVANCE(SB), NOSPLIT, $0-24
	MOVQ point+0(FP), AX
	MOVQ scratch+8(FP), BX
	MOVQ step+16(FP), CX
	MOVQ AX, X2
	MOVQ BX, X1
	MOVQ CX, X3
	ADD(X2, 32, X2, 0, X1, 0)
	SUBTRACT(X2, 32, X2, 0, X1, 32)
	MUL(X1, 0, X3, 0, X1, 0)
	MUL(X1, 32, X3, 32, X1, 32)
	MUL(X2, 96, X3, 64, X1, 64)
	ADD(X2, 64, X2, 64, X1, 96)
	SUBTRACT(X1, 0, X1, 32, X1, 128)
	ADD(X1, 0, X1, 32, X1, 160)
	SUBTRACT(X1, 96, X1, 64, X1, 192)
	ADD(X1, 96, X1, 64, X1, 224)
	MUL(X1, 128, X1, 192, X2, 0)
	MUL(X1, 160, X1, 224, X2, 32)
	MUL(X1, 192, X1, 224, X2, 64)
	MUL(X1, 128, X1, 160, X2, 96)
	RET
