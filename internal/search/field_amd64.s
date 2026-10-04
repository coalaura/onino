//go:build !purego

#include "textflag.h"
#include "field_amd64.h"

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
	PRODUCT()
	RET
