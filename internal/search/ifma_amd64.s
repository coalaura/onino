//go:build !purego

#include "textflag.h"
#define IFMA_STEP 64
#define IFMA_GROUPS 32
#include "ifma_body_amd64.h"
