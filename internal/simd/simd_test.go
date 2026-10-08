package simd

import "testing"

func TestPolicy(t *testing.T) {
	valid := []string{"auto", "portable", "bmi2", "bmi2-adx", "ifma"}

	for index, value := range valid {
		mode, err := Parse(value)
		if err != nil || mode != Mode(index) || mode.String() != value {
			t.Fatalf("%s: %v, %v", value, mode, err)
		}
	}

	invalid := []string{"", "AUTO", "avx512", "avx", "avx2", "avx2 ", "bmi2_adx", "generic"}

	for _, value := range invalid {
		_, err := Parse(value)
		if err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestRequiredFeatures(t *testing.T) {
	full := registers{maximum: 7, leaf1: 1<<26 | 1<<27 | 1<<28, leaf7b: 1<<16 | 1<<17 | 1<<21 | 1<<30 | 1<<31, leaf7c: 1 << 14, xcr0: 0xe6}
	if !decode(full).IFMA || !decode(full).VL || !decode(full).Keccak {
		t.Fatal("full capabilities rejected")
	}

	for bit := range 64 {
		if full.xcr0&(1<<bit) == 0 {
			continue
		}

		missing := full
		missing.xcr0 &^= 1 << bit

		if decode(missing).IFMA || decode(missing).Keccak {
			t.Fatalf("accepted missing XCR0 bit %d", bit)
		}
	}

	for bit := 26; bit <= 28; bit++ {
		missing := full
		missing.leaf1 &^= 1 << bit

		if decode(missing).IFMA || decode(missing).Keccak {
			t.Fatalf("accepted missing leaf1 bit %d", bit)
		}
	}

	bits := []uint{16, 17, 21, 30, 31}

	for _, bit := range bits {
		missing := full
		missing.leaf7b &^= 1 << bit

		features := decode(missing)
		if bit == 16 && (features.IFMA || features.Keccak) || bit == 21 && features.IFMA || bit == 31 && features.VL || bit == 17 && features.DQ || bit == 30 && features.BW {
			t.Fatalf("accepted missing leaf7 bit %d", bit)
		}
	}

	missing := full
	missing.leaf7c &^= 1 << 14

	if decode(missing).VPOPCNTDQ || !decode(missing).IFMA || !decode(missing).Keccak {
		t.Fatal("VPOPCNTDQ not gated independently")
	}

	full.maximum = 6

	if decode(full) != (Features{Known: true}) {
		t.Fatal("accepted unavailable leaf7")
	}
}

func TestHostFeatures(t *testing.T) {
	t.Logf("CPU capabilities: %+v", Detect())
}

func TestScalarFeaturesWithoutVectorState(t *testing.T) {
	value := registers{maximum: 7, leaf7b: 1<<5 | 1<<8 | 1<<19 | 1<<21}

	features := decode(value)
	if !features.BMI2 || !features.ADX || features.AVX2 || features.IFMA || features.Keccak || !features.AVX2CPU || !features.IFMACPU {
		t.Fatalf("incorrect independent detection: %+v", features)
	}

	value.leaf7b &^= 1 << 19

	features = decode(value)
	if !features.BMI2 || features.ADX {
		t.Fatal("BMI2 incorrectly depends on ADX")
	}

	value.leaf1 = 1<<26 | 1<<27 | 1<<28
	value.xcr0 = 6

	features = decode(value)
	if !features.AVX2 || features.IFMA {
		t.Fatal("AVX2 incorrectly depends on AVX-512 state")
	}
}
