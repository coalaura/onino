// Exactly 256 disjoint 96-byte centers and 224-byte scratch records; offset
// is immutable and its xy slot contains d*xQ*yQ. X registers preserve pointers
// across the multiplication core; R14 and X15 remain untouched.
TEXT PREPARE(SB), NOSPLIT, $0-24
	MOVQ center+0(FP), AX
	MOVQ scratch+8(FP), BX
	MOVQ offset+16(FP), CX
	MOVQ AX, X1
	MOVQ BX, X2
	MOVQ CX, X3
	MOVQ $256, DI
	MOVQ DI, X4
	LEAQ ·pairedOne(SB), SI
	MOVQ SI, X5
paired_prepare_loop:
	MUL(X1, 0, X3, 0, X2, 0)
	MUL(X1, 32, X3, 32, X2, 32)
	MUL(X1, 64, X3, 64, X2, 64)
	SQR(X2, 64, X2, 96)
	SUBTRACT(X5, 0, X2, 96, X2, 96)
	MOVQ X4, DI
	CMPQ DI, $256
	JE paired_prepare_first
	MUL(X2, -96, X2, 96, X2, 128)
	JMP paired_prepare_next
paired_prepare_first:
	MOVQ X2, AX
	MOVOU 96(AX), X6
	MOVOU 112(AX), X7
	MOVOU X6, 128(AX)
	MOVOU X7, 144(AX)
paired_prepare_next:
	MOVQ X4, DI
	DECQ DI
	JZ paired_prepare_done
	MOVQ DI, X4
	MOVQ X1, AX
	ADDQ $96, AX
	MOVQ AX, X1
	MOVQ X2, AX
	ADDQ $224, AX
	MOVQ AX, X2
	JMP paired_prepare_loop
paired_prepare_done:
	RET

// Scratch points to its last record. Inverse is separate caller-owned storage
// containing the inverse total denominator. Expired product/denominator slots
// hold the reciprocal and factors; both reconstructed inverses survive for X.
TEXT INVERSE(SB), NOSPLIT, $0-16
	MOVQ scratch+0(FP), AX
	MOVQ inverse+8(FP), BX
	MOVQ AX, X1
	MOVQ BX, X2
	MOVQ $256, DI
	MOVQ DI, X4
paired_inverse_loop:
	MOVQ X4, DI
	CMPQ DI, $1
	JE paired_inverse_first
	MUL(X2, 0, X1, -96, X1, 128)
	MUL(X2, 0, X1, 96, X2, 0)
	JMP paired_inverse_factors
paired_inverse_first:
	MOVQ X2, AX
	MOVQ X1, BX
	MOVOU 0(AX), X6
	MOVOU 16(AX), X7
	MOVOU X6, 128(BX)
	MOVOU X7, 144(BX)
paired_inverse_factors:
	// Reuse the expired denominator for c*r; reconstruct r +/- c*r.
	MUL(X1, 64, X1, 128, X1, 96)
	ADD(X1, 128, X1, 96, X1, 160)
	SUBTRACT(X1, 128, X1, 96, X1, 192)
	MOVQ X4, DI
	DECQ DI
	JZ paired_inverse_done
	MOVQ DI, X4
	MOVQ X1, AX
	SUBQ $224, AX
	MOVQ AX, X1
	JMP paired_inverse_loop
paired_inverse_done:
	RET
