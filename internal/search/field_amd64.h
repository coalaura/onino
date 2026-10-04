// Full-width BMI2/ADX multiplication, shared by the scalar and fused kernels.
// The upper 256 bits fold by 38, leaving an overflow of at most 38. Adding
// overflow*38 can wrap to at most 1443, so the final +38 cannot carry beyond
// the first limb. Inputs may alias the output. X0 holds the output pointer;
// R14 and X15 remain untouched.
// Constant displacements avoid pointer additions, including ADD $0, in fused leaves.
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
	XORL R13, R13; \
	MOVQ LO+8(BX), DX; \
	MULXQ RO+0(CX), AX, DI; \
	ADCXQ AX, R9; \
	ADOXQ DI, R10; \
	MULXQ RO+8(CX), AX, DI; \
	ADCXQ AX, R10; \
	ADOXQ DI, R11; \
	MULXQ RO+16(CX), AX, DI; \
	ADCXQ AX, R11; \
	ADOXQ DI, R12; \
	MULXQ RO+24(CX), AX, DI; \
	ADCXQ AX, R12; \
	ADOXQ DI, R13; \
	MOVQ $0, AX; \
	ADCXQ AX, R13; \
	XORL R15, R15; \
	MOVQ LO+16(BX), DX; \
	MULXQ RO+0(CX), AX, DI; \
	ADCXQ AX, R10; \
	ADOXQ DI, R11; \
	MULXQ RO+8(CX), AX, DI; \
	ADCXQ AX, R11; \
	ADOXQ DI, R12; \
	MULXQ RO+16(CX), AX, DI; \
	ADCXQ AX, R12; \
	ADOXQ DI, R13; \
	MULXQ RO+24(CX), AX, DI; \
	ADCXQ AX, R13; \
	ADOXQ DI, R15; \
	MOVQ $0, AX; \
	ADCXQ AX, R15; \
	XORL SI, SI; \
	MOVQ LO+24(BX), DX; \
	MULXQ RO+0(CX), AX, DI; \
	ADCXQ AX, R11; \
	ADOXQ DI, R12; \
	MULXQ RO+8(CX), AX, DI; \
	ADCXQ AX, R12; \
	ADOXQ DI, R13; \
	MULXQ RO+16(CX), AX, DI; \
	ADCXQ AX, R13; \
	ADOXQ DI, R15; \
	MULXQ RO+24(CX), AX, DI; \
	ADCXQ AX, R15; \
	ADOXQ DI, SI; \
	MOVQ $0, AX; \
	ADCXQ AX, SI; \
	MOVQ $38, DX; \
	XORL CX, CX; \
	MULXQ R12, AX, DI; \
	ADCXQ AX, R8; \
	MULXQ R13, AX, BX; \
	ADCXQ DI, R9; \
	ADOXQ AX, R9; \
	MULXQ R15, AX, DI; \
	ADCXQ BX, R10; \
	ADOXQ AX, R10; \
	MULXQ SI, AX, BX; \
	ADCXQ DI, R11; \
	ADOXQ AX, R11; \
	ADCXQ CX, BX; \
	ADOXQ CX, BX; \
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

#define LOAD(L, LO, R, RO) \
	MOVQ L, BX; \
	MOVQ R, CX; \
	MOVQ LO+0(BX), R8; \
	MOVQ LO+8(BX), R9; \
	MOVQ LO+16(BX), R10; \
	MOVQ LO+24(BX), R11

#define STORE(D, DO) \
	MOVQ D, AX; \
	MOVQ R8, DO+0(AX); \
	MOVQ R9, DO+8(AX); \
	MOVQ R10, DO+16(AX); \
	MOVQ R11, DO+24(AX)

#define ADD(L, LO, R, RO, D, DO) \
	LOAD(L, LO, R, RO); \
	ADDQ RO+0(CX), R8; \
	ADCQ RO+8(CX), R9; \
	ADCQ RO+16(CX), R10; \
	ADCQ RO+24(CX), R11; \
	SBBQ DX, DX; \
	ANDQ $38, DX; \
	ADDQ DX, R8; \
	ADCQ $0, R9; \
	ADCQ $0, R10; \
	ADCQ $0, R11; \
	SBBQ DX, DX; \
	ANDQ $38, DX; \
	ADDQ DX, R8; \
	STORE(D, DO)

#define SUBTRACT(L, LO, R, RO, D, DO) \
	LOAD(L, LO, R, RO); \
	SUBQ RO+0(CX), R8; \
	SBBQ RO+8(CX), R9; \
	SBBQ RO+16(CX), R10; \
	SBBQ RO+24(CX), R11; \
	SBBQ DX, DX; \
	ANDQ $38, DX; \
	SUBQ DX, R8; \
	SBBQ $0, R9; \
	SBBQ $0, R10; \
	SBBQ $0, R11; \
	SBBQ DX, DX; \
	ANDQ $38, DX; \
	SUBQ DX, R8; \
	STORE(D, DO)

#define MUL(L, LO, R, RO, D, DO) \
	MOVQ L, BX; \
	MOVQ R, CX; \
	MOVQ D, AX; \
	PRODUCT(LO, RO, DO)
