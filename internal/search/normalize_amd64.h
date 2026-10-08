// Pointers start at the last element of disjoint point, product and key arrays.
// Count is positive; reciprocal is separate storage holding the inverse total Z.
// The expired prefix product becomes inverse Z; projective coordinates stay intact.
TEXT NORMALIZE(SB), NOSPLIT, $0-40
	MOVQ point+0(FP), AX
	MOVQ product+8(FP), BX
	MOVQ publicKey+16(FP), CX
	MOVQ count+24(FP), DI
	MOVQ reciprocal+32(FP), SI
	MOVQ AX, X1
	MOVQ BX, X2
	MOVQ CX, X3
	MOVQ DI, X4
	MOVQ SI, X5
normalize_loop:
	MOVQ X4, DI
	CMPQ DI, $1
	JE normalize_first
	MUL(X5, 0, X2, -32, X2, 0)
	MUL(X5, 0, X1, 64, X5, 0)
	JMP normalize_y
normalize_first:
	MOVQ X5, SI
	MOVQ X2, DI
	MOVOU 0(SI), X6
	MOVOU 16(SI), X7
	MOVOU X6, 0(DI)
	MOVOU X7, 16(DI)
normalize_y:
	MUL(X1, 32, X2, 0, X3, 0)
	MOVQ R11, DX
	SHRQ $63, DX
	IMULQ $19, DX
	BTRQ $63, R11
	ADDQ DX, R8
	ADCQ $0, R9
	ADCQ $0, R10
	ADCQ $0, R11
	MOVQ R8, BX
	MOVQ R9, CX
	MOVQ R10, DX
	MOVQ R11, SI
	ADDQ $19, BX
	ADCQ $0, CX
	ADCQ $0, DX
	ADCQ $0, SI
	BTRQ $63, SI
	CMOVQCS BX, R8
	CMOVQCS CX, R9
	CMOVQCS DX, R10
	CMOVQCS SI, R11
	BTRQ $63, R11
	MOVQ X3, AX
	MOVQ R8, 0(AX)
	MOVQ R9, 8(AX)
	MOVQ R10, 16(AX)
	MOVQ R11, 24(AX)
	MOVQ X4, DI
	DECQ DI
	JZ normalize_done
	MOVQ DI, X4
	MOVQ X1, AX
	SUBQ $128, AX
	MOVQ AX, X1
	MOVQ X2, AX
	SUBQ $32, AX
	MOVQ AX, X2
	MOVQ X3, AX
	SUBQ $32, AX
	MOVQ AX, X3
	JMP normalize_loop
normalize_done:
	RET
