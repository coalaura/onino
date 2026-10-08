//go:build !purego

#include "textflag.h"
#include "field_amd64.h"
#include "field_bmi2_amd64.h"

TEXT ·multiplyBMI2Only(SB), NOSPLIT, $0-24
	MOVQ result+0(FP), AX
	MOVQ left+8(FP), BX
	MOVQ right+16(FP), CX
	PRODUCT(0, 0, 0)
	RET

TEXT ·squareBMI2Only(SB), NOSPLIT, $0-16
	MOVQ result+0(FP), AX
	MOVQ source+8(FP), BX
	SQUARE(0, 0)
	RET

#define PREPARE ·pairedPrepareBMI2Only
#define INVERSE ·pairedInverseBMI2Only
#define ADVANCE ·advanceBMI2Only
#define NORMALIZE ·normalizeBMI2Only
#include "paired_amd64.h"
#include "point_amd64.h"
#include "normalize_amd64.h"
