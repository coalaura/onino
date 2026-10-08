// Full-width MULX arithmetic using only ordinary carry instructions. The
// accumulator row invariant is product + old limb + incoming carry < 2^128.
// Keep each outgoing carry in a register until the next limb consumes it.
#undef PRODUCT
#undef REDUCE
#undef SQUARE
#define PRODUCT(LO, RO, DO) \
	MOVQ AX, X0; \
	MOVQ LO+0(BX), DX; \
	MULXQ RO+0(CX), R8, R9; \
	MULXQ RO+8(CX), AX, R10; \
	ADDQ AX, R9; \
	MULXQ RO+16(CX), AX, R11; \
	ADCQ AX, R10; \
	MULXQ RO+24(CX), AX, R12; \
	ADCQ AX, R11; \
	ADCQ $0, R12; \
	MOVQ LO+8(BX), DX; \
	MULXQ RO+0(CX), AX, DI; \
	ADDQ AX, R9; \
	ADCQ $0, DI; \
	MULXQ RO+8(CX), AX, R13; \
	ADDQ DI, R10; \
	ADCQ $0, R13; \
	ADDQ AX, R10; \
	ADCQ $0, R13; \
	MULXQ RO+16(CX), AX, DI; \
	ADDQ R13, R11; \
	ADCQ $0, DI; \
	ADDQ AX, R11; \
	ADCQ $0, DI; \
	MULXQ RO+24(CX), AX, R13; \
	ADDQ DI, R12; \
	ADCQ $0, R13; \
	ADDQ AX, R12; \
	ADCQ $0, R13; \
	MOVQ LO+16(BX), DX; \
	MULXQ RO+0(CX), AX, DI; \
	ADDQ AX, R10; \
	ADCQ $0, DI; \
	MULXQ RO+8(CX), AX, R15; \
	ADDQ DI, R11; \
	ADCQ $0, R15; \
	ADDQ AX, R11; \
	ADCQ $0, R15; \
	MULXQ RO+16(CX), AX, DI; \
	ADDQ R15, R12; \
	ADCQ $0, DI; \
	ADDQ AX, R12; \
	ADCQ $0, DI; \
	MULXQ RO+24(CX), AX, R15; \
	ADDQ DI, R13; \
	ADCQ $0, R15; \
	ADDQ AX, R13; \
	ADCQ $0, R15; \
	MOVQ LO+24(BX), DX; \
	MULXQ RO+0(CX), AX, DI; \
	ADDQ AX, R11; \
	ADCQ $0, DI; \
	MULXQ RO+8(CX), AX, SI; \
	ADDQ DI, R12; \
	ADCQ $0, SI; \
	ADDQ AX, R12; \
	ADCQ $0, SI; \
	MULXQ RO+16(CX), AX, DI; \
	ADDQ SI, R13; \
	ADCQ $0, DI; \
	ADDQ AX, R13; \
	ADCQ $0, DI; \
	MULXQ RO+24(CX), AX, SI; \
	ADDQ DI, R15; \
	ADCQ $0, SI; \
	ADDQ AX, R15; \
	ADCQ $0, SI; \
	REDUCE(DO)

// Fold the upper half by 38. The final overflow is at most 38; its
// correction can wrap to at most 1443, so one last +38 cannot carry.
#define REDUCE(DO) \
	MOVQ $38, DX; \
	MULXQ R12, AX, DI; \
	ADDQ AX, R8; \
	ADCQ $0, DI; \
	MULXQ R13, AX, BX; \
	ADDQ DI, R9; \
	ADCQ $0, BX; \
	ADDQ AX, R9; \
	ADCQ $0, BX; \
	MULXQ R15, AX, DI; \
	ADDQ BX, R10; \
	ADCQ $0, DI; \
	ADDQ AX, R10; \
	ADCQ $0, DI; \
	MULXQ SI, AX, BX; \
	ADDQ DI, R11; \
	ADCQ $0, BX; \
	ADDQ AX, R11; \
	ADCQ $0, BX; \
	IMULQ $38, BX; \
	ADDQ BX, R8; \
	ADCQ $0, R9; \
	ADCQ $0, R10; \
	ADCQ $0, R11; \
	SBBQ BX, BX; \
	ANDQ $38, BX; \
	ADDQ BX, R8; \
	MOVQ X0, AX; \
	MOVQ R8, DO+0(AX); \
	MOVQ R9, DO+8(AX); \
	MOVQ R10, DO+16(AX); \
	MOVQ R11, DO+24(AX)

// Six cross-products, doubled before the four diagonals. Consume every
// source limb before storing, including the noncanonical top input bit.
#define SQUARE(LO, DO) \
	MOVQ AX, X0; \
	MOVQ LO+0(BX), DX; \
	MULXQ LO+8(BX), R9, R10; \
	MULXQ LO+16(BX), AX, R11; \
	ADDQ AX, R10; \
	MULXQ LO+24(BX), AX, R12; \
	ADCQ AX, R11; \
	ADCQ $0, R12; \
	MOVQ $0, R13; \
	MOVQ LO+8(BX), DX; \
	MULXQ LO+16(BX), AX, DI; \
	ADDQ AX, R11; \
	ADCQ DI, R12; \
	ADCQ $0, R13; \
	MULXQ LO+24(BX), AX, DI; \
	ADDQ AX, R12; \
	ADCQ DI, R13; \
	MOVQ LO+16(BX), DX; \
	MULXQ LO+24(BX), AX, R15; \
	ADDQ AX, R13; \
	ADCQ $0, R15; \
	XORL SI, SI; \
	ADDQ R9, R9; \
	ADCQ R10, R10; \
	ADCQ R11, R11; \
	ADCQ R12, R12; \
	ADCQ R13, R13; \
	ADCQ R15, R15; \
	ADCQ $0, SI; \
	MOVQ LO+0(BX), DX; \
	MULXQ DX, R8, DI; \
	ADDQ DI, R9; \
	MOVQ LO+8(BX), DX; \
	MULXQ DX, AX, DI; \
	ADCQ AX, R10; \
	ADCQ DI, R11; \
	MOVQ LO+16(BX), DX; \
	MULXQ DX, AX, DI; \
	ADCQ AX, R12; \
	ADCQ DI, R13; \
	MOVQ LO+24(BX), DX; \
	MULXQ DX, AX, DI; \
	ADCQ AX, R15; \
	ADCQ DI, SI; \
	REDUCE(DO)
