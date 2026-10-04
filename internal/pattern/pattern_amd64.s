#include "textflag.h"

// Extract eight overlapping 15-bit windows. Each shuffle builds a big-endian
// 24-bit value in a little-endian dword, then shifts by 9-(position*5)%8.
#define WINDOW(offset, source, target) \
	VPSHUFB shuffle<>+offset(SB), source, target; \
	VPSRLVD Y13, target, target; \
	VPAND Y12, target, target

// Groups start at positions 0,8,...,48 and use input windows starting at bytes
// 0,0,8,8,16,16,24. A dword's shuffle bytes are {byte+2,byte+1,byte,0x80}.
DATA shuffle<>+0(SB)/8, $0x8000010280000102
DATA shuffle<>+8(SB)/8, $0x8001020380010203
DATA shuffle<>+16(SB)/8, $0x8003040580020304
DATA shuffle<>+24(SB)/8, $0x8004050680030405
DATA shuffle<>+32(SB)/8, $0x8005060780050607
DATA shuffle<>+40(SB)/8, $0x8006070880060708
DATA shuffle<>+48(SB)/8, $0x8008090a80070809
DATA shuffle<>+56(SB)/8, $0x80090a0b8008090a
DATA shuffle<>+64(SB)/8, $0x8002030480020304
DATA shuffle<>+72(SB)/8, $0x8003040580030405
DATA shuffle<>+80(SB)/8, $0x8005060780040506
DATA shuffle<>+88(SB)/8, $0x8006070880050607
DATA shuffle<>+96(SB)/8, $0x8007080980070809
DATA shuffle<>+104(SB)/8, $0x8008090a8008090a
DATA shuffle<>+112(SB)/8, $0x800a0b0c80090a0b
DATA shuffle<>+120(SB)/8, $0x800b0c0d800a0b0c
DATA shuffle<>+128(SB)/8, $0x8004050680040506
DATA shuffle<>+136(SB)/8, $0x8005060780050607
DATA shuffle<>+144(SB)/8, $0x8007080980060708
DATA shuffle<>+152(SB)/8, $0x8008090a80070809
DATA shuffle<>+160(SB)/8, $0x80090a0b80090a0b
DATA shuffle<>+168(SB)/8, $0x800a0b0c800a0b0c
DATA shuffle<>+176(SB)/8, $0x800c0d0e800b0c0d
DATA shuffle<>+184(SB)/8, $0x800d0e0f800c0d0e
DATA shuffle<>+192(SB)/8, $0x8006070880060708
DATA shuffle<>+200(SB)/8, $0x8007080980070809
DATA shuffle<>+208(SB)/8, $0x80090a0b8008090a
DATA shuffle<>+216(SB)/8, $0x800a0b0c80090a0b
GLOBL shuffle<>(SB), RODATA|NOPTR, $224

DATA shift<>+0(SB)/8, $0x0000000400000009
DATA shift<>+8(SB)/8, $0x0000000200000007
DATA shift<>+16(SB)/8, $0x0000000800000005
DATA shift<>+24(SB)/8, $0x0000000600000003
GLOBL shift<>(SB), RODATA|NOPTR, $32

DATA windowMask<>+0(SB)/4, $0x00007fff
GLOBL windowMask<>(SB), RODATA|NOPTR, $4

TEXT ·supportsAVX2(SB), NOSPLIT, $0-1
	MOVB $0, ret+0(FP)
	XORL AX, AX
	CPUID
	CMPL AX, $7
	JB no_avx2
	MOVL $1, AX
	CPUID
	ANDL $0x18000000, CX
	CMPL CX, $0x18000000
	JNE no_avx2
	XORL CX, CX
	XGETBV
	ANDL $6, AX
	CMPL AX, $6
	JNE no_avx2
	MOVL $7, AX
	XORL CX, CX
	CPUID
	TESTL $0x20, BX
	JZ no_avx2
	MOVB $1, ret+0(FP)
no_avx2:
	RET

// Load the input exactly once, without overreading. All later input work stays
// in registers. Zero-filled final windows supply canonical base32 padding bits.
TEXT ·filterCandidates(SB), NOSPLIT, $0-40
	MOVQ data+0(FP), AX
	MOVQ plans+8(FP), BX
	MOVQ count+16(FP), CX
	VMOVDQU (AX), Y0
	VPERM2I128 $0x00, Y0, Y0, Y2
	VPERM2I128 $0x11, Y0, Y0, Y4
	VPERM2I128 $0x81, Y0, Y0, Y1
	VPALIGNR $8, Y0, Y1, Y3
	VPERM2I128 $0x00, Y3, Y3, Y3
	VPSRLDQ $8, Y4, Y5
	VPBROADCASTD windowMask<>(SB), Y12
	VMOVDQU shift<>(SB), Y13
	WINDOW(0, Y2, Y6)
	WINDOW(32, Y2, Y7)
	VPACKSSDW Y7, Y6, Y8
	WINDOW(64, Y3, Y6)
	WINDOW(96, Y3, Y7)
	VPACKSSDW Y7, Y6, Y9
	WINDOW(128, Y4, Y6)
	WINDOW(160, Y4, Y7)
	VPACKSSDW Y7, Y6, Y10
	WINDOW(192, Y5, Y6)
	VPXOR Y7, Y7, Y7
	VPACKSSDW Y7, Y6, Y11
	XORQ R10, R10
plan_loop:
	VPBROADCASTD 12(BX), Y7
	// Only two-symbol literals need masking; other windows are already 15-bit.
	CMPL 8(BX), $0x7fff7fff
	JNE short_plan
	VPCMPEQW Y7, Y8, Y0
	VPCMPEQW Y7, Y9, Y1
	VPCMPEQW Y7, Y10, Y2
	VPCMPEQW Y7, Y11, Y3
pack_matches:
	VPACKSSWB Y1, Y0, Y0
	VPACKSSWB Y3, Y2, Y2
	VPMOVMSKB Y0, R8
	VPMOVMSKB Y2, R9
	SHLQ $32, R9
	ORQ R9, R8
	ANDQ 0(BX), R8
	JNZ candidates_found
	ADDQ $16, BX
	INCQ R10
	CMPQ R10, CX
	JNE plan_loop
	MOVQ $-1, R10
candidates_found:
	MOVQ R10, index+24(FP)
	MOVQ R8, candidates+32(FP)
	VZEROUPPER
	RET

short_plan:
	VPBROADCASTD 8(BX), Y6
	VPAND Y8, Y6, Y0
	VPAND Y9, Y6, Y1
	VPAND Y10, Y6, Y2
	VPAND Y11, Y6, Y3
	VPCMPEQW Y7, Y0, Y0
	VPCMPEQW Y7, Y1, Y1
	VPCMPEQW Y7, Y2, Y2
	VPCMPEQW Y7, Y3, Y3
	JMP pack_matches
