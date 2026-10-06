//go:build !purego

#include "textflag.h"
#define IFMA_STEP 64
#include "ifma_amd64.h"

// Z0..Z4 are normalized. Add 19 only when the full carry chain proves >= p,
// then discard the 2^255 carry. Only the low word is needed for filtering;
// STORE_SURVIVORS finishes the remaining carries. Z26 holds the radix mask.
#define CANONICAL \
	MOVQ $19, DX; \
	VPBROADCASTQ DX, Z5; \
	VPADDQ Z0, Z5, Z6; \
	VPSRLQ $51, Z6, Z6; \
	VPADDQ Z1, Z6, Z6; \
	VPSRLQ $51, Z6, Z6; \
	VPADDQ Z2, Z6, Z6; \
	VPSRLQ $51, Z6, Z6; \
	VPADDQ Z3, Z6, Z6; \
	VPSRLQ $51, Z6, Z6; \
	VPADDQ Z4, Z6, Z6; \
	VPSRLQ $51, Z6, Z6; \
	VPSLLQ $4, Z6, Z7; \
	VPADDQ Z6, Z7, Z7; \
	VPADDQ Z6, Z7, Z7; \
	VPADDQ Z6, Z7, Z7; \
	VPADDQ Z7, Z0, Z0; \
	CARRY(Z0, Z1)

#define RESULT_LEFT \
	VMOVDQA64 Z0, Z16; \
	VMOVDQA64 Z1, Z17; \
	VMOVDQA64 Z2, Z18; \
	VMOVDQA64 Z3, Z19; \
	VMOVDQA64 Z4, Z20

#define STORE_SURVIVORS \
	CARRY(Z1, Z2); \
	CARRY(Z2, Z3); \
	CARRY(Z3, Z4); \
	VPANDQ Z26, Z4, Z4; \
	KMOVW R9, K1; \
	VMOVDQU64 Z0, K1, 0(AX); \
	VMOVDQU64 Z1, K1, 64(AX); \
	VMOVDQU64 Z2, K1, 128(AX); \
	VMOVDQU64 Z3, K1, 192(AX); \
	VMOVDQU64 Z4, K1, 256(AX)

// Spread eight bits without requiring BMI2 in the AVX-512F+IFMA feature gate.
#define SPREAD(REG) \
	MOVQ REG, DX; \
	SHLQ $4, REG; \
	ORQ DX, REG; \
	ANDQ $0x0f0f, REG; \
	MOVQ REG, DX; \
	SHLQ $2, REG; \
	ORQ DX, REG; \
	ANDQ $0x3333, REG; \
	MOVQ REG, DX; \
	SHLQ $1, REG; \
	ORQ DX, REG; \
	ANDQ $0x5555, REG

TEXT ·ifmaPairedFilter(SB), NOSPLIT, $0-40
	MOVQ plus+0(FP), AX
	MOVQ minus+8(FP), R10
	MOVQ scratch+16(FP), R11
	MOVQ plan+24(FP), R8
	LEAQ 320(R11), BX
	MOVQ R11, CX
	LOAD_LEFT
	LOAD_RIGHT
	SUB
	NORMALIZE
	// These five registers survive the multiply, normalization and filter.
	VMOVDQA64 Z0, Z13
	VMOVDQA64 Z1, Z14
	VMOVDQA64 Z2, Z28
	VMOVDQA64 Z3, Z29
	VMOVDQA64 Z4, Z31
	ADD
	NORMALIZE
	RESULT_LEFT
	LEAQ 1600(R11), CX
	LOAD_RIGHT
	MULTIPLY
	JOIN
	NORMALIZE
	CANONICAL
	VPSLLQ $51, Z1, Z5
	VPORQ Z0, Z5, Z5
	MOVQ R8, DI
	MOVQ 128(DI), CX
	XORQ R9, R9
paired_plus_filter:
	VPBROADCASTQ 0(DI), Z6
	VPBROADCASTQ 8(DI), Z7
	VPANDQ Z5, Z6, Z6
	VPCMPQ $0, Z7, Z6, K1
	KMOVW K1, DX
	ORQ DX, R9
	ADDQ $16, DI
	DECQ CX
	JNZ paired_plus_filter
	TESTQ R9, R9
	JZ paired_minus
	STORE_SURVIVORS
paired_minus:
	MOVQ R9, SI
	MOVQ R10, AX
	VMOVDQA64 Z13, Z16
	VMOVDQA64 Z14, Z17
	VMOVDQA64 Z28, Z18
	VMOVDQA64 Z29, Z19
	VMOVDQA64 Z31, Z20
	LEAQ 1920(R11), CX
	LOAD_RIGHT
	MULTIPLY
	JOIN
	NORMALIZE
	CANONICAL
	VPSLLQ $51, Z1, Z5
	VPORQ Z0, Z5, Z5
	MOVQ R8, DI
	MOVQ 128(DI), CX
	XORQ R9, R9
paired_minus_filter:
	VPBROADCASTQ 0(DI), Z6
	VPBROADCASTQ 8(DI), Z7
	VPANDQ Z5, Z6, Z6
	VPCMPQ $0, Z7, Z6, K1
	KMOVW K1, DX
	ORQ DX, R9
	ADDQ $16, DI
	DECQ CX
	JNZ paired_minus_filter
	TESTQ R9, R9
	JZ paired_mask
	STORE_SURVIVORS
paired_mask:
	MOVQ SI, DX
	ORQ R9, DX
	JZ paired_return
	SPREAD(SI)
	SPREAD(R9)
	SHLQ $1, R9
	ORQ SI, R9
paired_return:
	MOVQ R9, mask+32(FP)
	VZEROUPPER
	RET
