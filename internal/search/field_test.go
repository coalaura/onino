package search

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestFieldArithmetic(t *testing.T) {
	values := []fieldElement{
		{},
		{1, 0, 0, 0},
		{0xffffffffffffffff, 0, 0, 0},
		{0, 0, 0, 0x8000000000000000},
		{0xffffffffffffffec, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffed, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffee, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffd9, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		{0xffffffffffffffda, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
	}

	for _, left := range values {
		for _, right := range values {
			checkFieldArithmetic(t, left, right)
		}
	}

	random := rand.New(rand.NewPCG(37, 91))

	for range 20000 {
		left := fieldElement{random.Uint64(), random.Uint64(), random.Uint64(), random.Uint64()}
		right := fieldElement{random.Uint64(), random.Uint64(), random.Uint64(), random.Uint64()}

		checkFieldArithmetic(t, left, right)
	}
}

func FuzzFieldArithmetic(f *testing.F) {
	f.Add(make([]byte, 32), make([]byte, 32))

	maximal := make([]byte, 32)

	for index := range maximal {
		maximal[index] = 255
	}

	f.Add(maximal, maximal)

	f.Fuzz(func(t *testing.T, leftBytes, rightBytes []byte) {
		if len(leftBytes) != 32 || len(rightBytes) != 32 {
			return
		}

		var (
			left  fieldElement
			right fieldElement
		)

		for index := range left {
			left[index] = binary.LittleEndian.Uint64(leftBytes[index*8:])
			right[index] = binary.LittleEndian.Uint64(rightBytes[index*8:])
		}

		checkFieldArithmetic(t, left, right)
	})
}

func TestGenericFieldCarryChains(t *testing.T) {
	prime := new(big.Int).Lsh(big.NewInt(1), 255)
	prime.Sub(prime, big.NewInt(19))

	values := make([]fieldElement, 0, 515)
	values = append(values, fieldElement{}, fieldElement{1}, fieldElement{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)})

	for limb := range 4 {
		for bit := range 64 {
			var value fieldElement

			value[limb] = uint64(1) << bit
			values = append(values, value)
			value[limb]--
			values = append(values, value)
		}
	}

	for _, value := range values {
		actual := value
		expected := fieldInteger(value)

		for round := range 32 {
			squareGeneric(&actual, &actual)
			expected.Mul(expected, expected)
			expected.Mod(expected, prime)
			checkFieldResult(t, "repeated generic square", &actual, expected)

			factor := values[(round*17+3)%len(values)]
			factorInteger := fieldInteger(factor)
			expected.Mul(expected, factorInteger)
			expected.Mod(expected, prime)

			rightAlias := factor
			multiplyGeneric(&rightAlias, &actual, &rightAlias)
			multiplyGeneric(&actual, &actual, &factor)
			checkFieldResult(t, "repeated generic left alias", &actual, expected)
			checkFieldResult(t, "repeated generic right alias", &rightAlias, expected)
		}
	}
}

func checkFieldArithmetic(t *testing.T, left, right fieldElement) {
	t.Helper()

	prime := new(big.Int).Lsh(big.NewInt(1), 255)

	prime.Sub(prime, big.NewInt(19))

	leftInteger := fieldInteger(left)
	rightInteger := fieldInteger(right)

	var result fieldElement

	checkFieldResult(t, "canonical", &left, new(big.Int).Mod(leftInteger, prime))

	result.add(&left, &right)

	expected := new(big.Int).Add(leftInteger, rightInteger)

	expected.Mod(expected, prime)

	checkFieldResult(t, "add", &result, expected)

	result.subtract(&left, &right)

	expected.Sub(leftInteger, rightInteger)
	expected.Mod(expected, prime)

	checkFieldResult(t, "subtract", &result, expected)

	expected.Mul(leftInteger, rightInteger)
	expected.Mod(expected, prime)

	multiplyGeneric(&result, &left, &right)
	checkFieldResult(t, "generic multiply", &result, expected)

	result.multiply(&left, &right)

	checkFieldResult(t, "multiply", &result, expected)

	result = left
	result.multiply(&result, &right)

	checkFieldResult(t, "left alias", &result, expected)

	result = right
	result.multiply(&left, &result)

	checkFieldResult(t, "right alias", &result, expected)

	result = left
	result.multiply(&result, &result)

	expected.Mul(leftInteger, leftInteger)
	expected.Mod(expected, prime)

	checkFieldResult(t, "in-place square", &result, expected)

	result.square(&left)

	checkFieldResult(t, "dedicated square", &result, expected)

	result = left
	result.square(&result)

	checkFieldResult(t, "dedicated in-place square", &result, expected)

	result.invert(&left)

	if result != result.canonical() {
		t.Fatal("inverse is not canonical")
	}

	if fieldInteger(left).Cmp(leftInteger) != 0 {
		t.Fatal("inversion changed its source")
	}

	expected.ModInverse(leftInteger, prime)

	if new(big.Int).Mod(leftInteger, prime).Sign() == 0 {
		expected.SetInt64(0)
	}

	checkFieldResult(t, "invert", &result, expected)

	result = left
	result.invert(&result)

	checkFieldResult(t, "in-place invert", &result, expected)
}

func checkFieldResult(t *testing.T, operation string, actual *fieldElement, expected *big.Int) {
	t.Helper()

	var encoded [32]byte

	actual.putBytes(&encoded)

	for index := range 16 {
		encoded[index], encoded[31-index] = encoded[31-index], encoded[index]
	}

	integer := new(big.Int).SetBytes(encoded[:])
	if integer.Cmp(expected) != 0 {
		t.Fatalf("%s: got %x, want %x", operation, integer, expected)
	}

	if actual.isNegative() != byte(expected.Bit(0)) {
		t.Fatalf("%s: incorrect sign bit", operation)
	}
}

func fieldInteger(value fieldElement) *big.Int {
	var encoded [32]byte

	for index := range value {
		binary.BigEndian.PutUint64(encoded[(3-index)*8:], value[index])
	}

	return new(big.Int).SetBytes(encoded[:])
}
