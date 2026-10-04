//go:build !purego

#include "textflag.h"

TEXT ·supportsBMI2ADX(SB), NOSPLIT, $0-1
	XORL AX, AX
	CPUID
	CMPL AX, $7
	JB unsupported
	MOVL $7, AX
	XORL CX, CX
	CPUID
	ANDL $0x80100, BX
	CMPL BX, $0x80100
	SETEQ ret+0(FP)
	RET
unsupported:
	MOVB $0, ret+0(FP)
	RET

// A 4x4 product using independent carry chains for low and high halves.
// R14 and X15 are untouched. X0 retains the output pointer until all inputs
// have been consumed, allowing either input to alias the output.
TEXT ·multiplyBMI2(SB), NOSPLIT, $0-24
	MOVQ result+0(FP), AX
	MOVQ left+8(FP), BX
	MOVQ right+16(FP), CX
	MOVQ AX, X0
	MOVQ 0(BX), DX
	MULXQ 0(CX), R8, R9
	MULXQ 8(CX), AX, R10
	ADDQ AX, R9
	MULXQ 16(CX), AX, R11
	ADCQ AX, R10
	MULXQ 24(CX), AX, R12
	ADCQ AX, R11
	ADCQ $0, R12
	XORL R13, R13
	MOVQ 8(BX), DX
	MULXQ 0(CX), AX, DI
	ADCXQ AX, R9
	ADOXQ DI, R10
	MULXQ 8(CX), AX, DI
	ADCXQ AX, R10
	ADOXQ DI, R11
	MULXQ 16(CX), AX, DI
	ADCXQ AX, R11
	ADOXQ DI, R12
	MULXQ 24(CX), AX, DI
	ADCXQ AX, R12
	ADOXQ DI, R13
	MOVQ $0, AX
	ADCXQ AX, R13
	XORL R15, R15
	MOVQ 16(BX), DX
	MULXQ 0(CX), AX, DI
	ADCXQ AX, R10
	ADOXQ DI, R11
	MULXQ 8(CX), AX, DI
	ADCXQ AX, R11
	ADOXQ DI, R12
	MULXQ 16(CX), AX, DI
	ADCXQ AX, R12
	ADOXQ DI, R13
	MULXQ 24(CX), AX, DI
	ADCXQ AX, R13
	ADOXQ DI, R15
	MOVQ $0, AX
	ADCXQ AX, R15
	XORL SI, SI
	MOVQ 24(BX), DX
	MULXQ 0(CX), AX, DI
	ADCXQ AX, R11
	ADOXQ DI, R12
	MULXQ 8(CX), AX, DI
	ADCXQ AX, R12
	ADOXQ DI, R13
	MULXQ 16(CX), AX, DI
	ADCXQ AX, R13
	ADOXQ DI, R15
	MULXQ 24(CX), AX, DI
	ADCXQ AX, R15
	ADOXQ DI, SI
	MOVQ $0, AX
	ADCXQ AX, SI

	// Fold the upper 256 bits with 2^256 = 38 (mod p).
	MOVQ $38, DX
	XORL CX, CX
	MULXQ R12, AX, DI
	ADCXQ AX, R8
	MULXQ R13, AX, BX
	ADCXQ DI, R9
	ADOXQ AX, R9
	MULXQ R15, AX, DI
	ADCXQ BX, R10
	ADOXQ AX, R10
	MULXQ SI, AX, BX
	ADCXQ DI, R11
	ADOXQ AX, R11
	ADCXQ CX, BX
	ADOXQ CX, BX
	IMULQ $38, BX
	ADDQ BX, R8
	ADCQ $0, R9
	ADCQ $0, R10
	ADCQ $0, R11
	// If folding overflowed, the wrapped value is at most 1443. The second
	// correction therefore cannot carry beyond its first limb.
	SBBQ BX, BX
	ANDQ $38, BX
	ADDQ BX, R8
	MOVQ X0, AX
	MOVQ R8, 0(AX)
	MOVQ R9, 8(AX)
	MOVQ R10, 16(AX)
	MOVQ R11, 24(AX)
	RET
