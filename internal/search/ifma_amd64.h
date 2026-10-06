// Eight independent radix-2^51 field operations. Never use X/Y/Z15 (Go's
// zero register) or R14. All sources are loaded before any destination store.
#define LOAD_LEFT \
	VMOVDQU64 0(BX), Z16; \
	VMOVDQU64 IFMA_STEP(BX), Z17; \
	VMOVDQU64 (2*IFMA_STEP)(BX), Z18; \
	VMOVDQU64 (3*IFMA_STEP)(BX), Z19; \
	VMOVDQU64 (4*IFMA_STEP)(BX), Z20

#define LOAD_RIGHT \
	VMOVDQU64 0(CX), Z21; \
	VMOVDQU64 IFMA_STEP(CX), Z22; \
	VMOVDQU64 (2*IFMA_STEP)(CX), Z23; \
	VMOVDQU64 (3*IFMA_STEP)(CX), Z24; \
	VMOVDQU64 (4*IFMA_STEP)(CX), Z25

#define ZERO_ACC \
	VPXORQ Z0, Z0, Z0; \
	VPXORQ Z1, Z1, Z1; \
	VPXORQ Z2, Z2, Z2; \
	VPXORQ Z3, Z3, Z3; \
	VPXORQ Z4, Z4, Z4; \
	VPXORQ Z5, Z5, Z5; \
	VPXORQ Z6, Z6, Z6; \
	VPXORQ Z7, Z7, Z7; \
	VPXORQ Z8, Z8, Z8; \
	VPXORQ Z9, Z9, Z9

#define PRODUCT(LOW, HIGH, LEFT, RIGHT) \
	VPMADD52LUQ RIGHT, LEFT, LOW; \
	VPMADD52HUQ RIGHT, LEFT, HIGH

#define CROSS(LOW, HIGH, LEFT, RIGHT) \
	VPSLLQ $1, LEFT, Z30; \
	PRODUCT(LOW, HIGH, Z30, RIGHT)

// Multiplying the accumulators, not multiplicands, keeps IFMA inputs in range.
#define TIMES19(REG) \
	VPSLLQ $4, REG, Z10; \
	VPSLLQ $1, REG, Z11; \
	VPADDQ Z10, REG, REG; \
	VPADDQ Z11, REG, REG

#define WRAP(LOW, HIGH) \
	TIMES19(LOW); \
	TIMES19(HIGH)

#define MULTIPLY \
	ZERO_ACC; \
	PRODUCT(Z0, Z5, Z17, Z25); \
	PRODUCT(Z0, Z5, Z18, Z24); \
	PRODUCT(Z0, Z5, Z19, Z23); \
	PRODUCT(Z0, Z5, Z20, Z22); \
	WRAP(Z0, Z5); \
	PRODUCT(Z0, Z5, Z16, Z21); \
	PRODUCT(Z1, Z6, Z18, Z25); \
	PRODUCT(Z1, Z6, Z19, Z24); \
	PRODUCT(Z1, Z6, Z20, Z23); \
	WRAP(Z1, Z6); \
	PRODUCT(Z1, Z6, Z16, Z22); \
	PRODUCT(Z1, Z6, Z17, Z21); \
	PRODUCT(Z2, Z7, Z19, Z25); \
	PRODUCT(Z2, Z7, Z20, Z24); \
	WRAP(Z2, Z7); \
	PRODUCT(Z2, Z7, Z16, Z23); \
	PRODUCT(Z2, Z7, Z17, Z22); \
	PRODUCT(Z2, Z7, Z18, Z21); \
	PRODUCT(Z3, Z8, Z20, Z25); \
	WRAP(Z3, Z8); \
	PRODUCT(Z3, Z8, Z16, Z24); \
	PRODUCT(Z3, Z8, Z17, Z23); \
	PRODUCT(Z3, Z8, Z18, Z22); \
	PRODUCT(Z3, Z8, Z19, Z21); \
	PRODUCT(Z4, Z9, Z16, Z25); \
	PRODUCT(Z4, Z9, Z17, Z24); \
	PRODUCT(Z4, Z9, Z18, Z23); \
	PRODUCT(Z4, Z9, Z19, Z22); \
	PRODUCT(Z4, Z9, Z20, Z21)

#define SQUARE \
	ZERO_ACC; \
	CROSS(Z0, Z5, Z17, Z20); \
	CROSS(Z0, Z5, Z18, Z19); \
	WRAP(Z0, Z5); \
	PRODUCT(Z0, Z5, Z16, Z16); \
	CROSS(Z1, Z6, Z18, Z20); \
	PRODUCT(Z1, Z6, Z19, Z19); \
	WRAP(Z1, Z6); \
	CROSS(Z1, Z6, Z16, Z17); \
	CROSS(Z2, Z7, Z19, Z20); \
	WRAP(Z2, Z7); \
	CROSS(Z2, Z7, Z16, Z18); \
	PRODUCT(Z2, Z7, Z17, Z17); \
	PRODUCT(Z3, Z8, Z20, Z20); \
	WRAP(Z3, Z8); \
	CROSS(Z3, Z8, Z16, Z19); \
	CROSS(Z3, Z8, Z17, Z18); \
	CROSS(Z4, Z9, Z16, Z20); \
	CROSS(Z4, Z9, Z17, Z19); \
	PRODUCT(Z4, Z9, Z18, Z18)

// The coefficient weight sum is at most 77. Low halves <77*2^52,
// high halves <77*2^50; neither accumulation nor the 19-fold can overflow.
// HI4 has weight 5, so adding 38*HI4 to LO0 stays below 2^60.
#define JOIN \
	VPSLLQ $1, Z5, Z5; \
	VPSLLQ $1, Z6, Z6; \
	VPSLLQ $1, Z7, Z7; \
	VPSLLQ $1, Z8, Z8; \
	VPSLLQ $1, Z9, Z9; \
	TIMES19(Z9); \
	VPADDQ Z9, Z0, Z0; \
	VPADDQ Z5, Z1, Z1; \
	VPADDQ Z6, Z2, Z2; \
	VPADDQ Z7, Z3, Z3; \
	VPADDQ Z8, Z4, Z4

#define CARRY(FROM, TO) \
	VPSRLQ $51, FROM, Z10; \
	VPANDQ Z26, FROM, FROM; \
	VPADDQ Z10, TO, TO

#define CARRY_ROUND \
	CARRY(Z0, Z1); \
	CARRY(Z1, Z2); \
	CARRY(Z2, Z3); \
	CARRY(Z3, Z4); \
	VPSRLQ $51, Z4, Z12; \
	VPANDQ Z26, Z4, Z4; \
	TIMES19(Z12); \
	VPADDQ Z12, Z0, Z0

// JOIN leaves every coefficient below 2^60, with ample room for incoming carries.
// One round leaves limbs 1..4 below B=2^51 and limb 0 below B+19*512.
// If round two carries from limb 0, its remainder is below 19*512; a full
// cascade can add only 19. Otherwise no limb can carry. Two rounds suffice.
#define NORMALIZE \
	MOVQ $0x7ffffffffffff, DX; \
	VPBROADCASTQ DX, Z26; \
	CARRY_ROUND; \
	CARRY_ROUND

#define STORE \
	VMOVDQU64 Z0, 0(AX); \
	VMOVDQU64 Z1, IFMA_STEP(AX); \
	VMOVDQU64 Z2, (2*IFMA_STEP)(AX); \
	VMOVDQU64 Z3, (3*IFMA_STEP)(AX); \
	VMOVDQU64 Z4, (4*IFMA_STEP)(AX)

#define MUL \
	LOAD_LEFT; LOAD_RIGHT; MULTIPLY; JOIN; NORMALIZE; STORE

#define ADD \
	VPADDQ Z16, Z21, Z0; \
	VPADDQ Z17, Z22, Z1; \
	VPADDQ Z18, Z23, Z2; \
	VPADDQ Z19, Z24, Z3; \
	VPADDQ Z20, Z25, Z4

#define SUB \
	MOVQ $0xfffffffffffda, DX; \
	VPBROADCASTQ DX, Z27; \
	VPADDQ Z27, Z16, Z0; \
	MOVQ $0xffffffffffffe, DX; \
	VPBROADCASTQ DX, Z27; \
	VPADDQ Z27, Z17, Z1; \
	VPADDQ Z27, Z18, Z2; \
	VPADDQ Z27, Z19, Z3; \
	VPADDQ Z27, Z20, Z4; \
	VPSUBQ Z21, Z0, Z0; \
	VPSUBQ Z22, Z1, Z1; \
	VPSUBQ Z23, Z2, Z2; \
	VPSUBQ Z24, Z3, Z3; \
	VPSUBQ Z25, Z4, Z4
