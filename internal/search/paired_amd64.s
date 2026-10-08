//go:build !purego

#include "textflag.h"
#include "field_amd64.h"
#define PREPARE ·pairedPrepareBMI2
#define INVERSE ·pairedInverseBMI2
#include "paired_amd64.h"
