package sign

import (
	"errors"
	"math"
	"math/big"
)

// The Bitcoin alphabet: digits and letters without 0, O, I and l, so a code
// read aloud or typed by hand has no look-alike characters.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Width is how many base58 digits a value of the given byte length
// always fits in: ceil(bits / log2(58)).
func base58Width(byteLength int) int {
	return int(math.Ceil(float64(8*byteLength) / math.Log2(58)))
}

// encodeBase58Fixed writes the bytes as one big-endian integer in exactly
// base58Width(len(data)) digits, left-padded with "1" (the digit zero), so
// every signature of one scheme has the same length and no separate length
// field or leading-zero convention is needed.
func encodeBase58Fixed(data []byte) (string, error) {
	width := base58Width(len(data))
	value := new(big.Int).SetBytes(data)
	digits := make([]byte, width)
	radix := big.NewInt(58)
	remainder := new(big.Int)
	for index := width - 1; index >= 0; index-- {
		value.QuoRem(value, radix, remainder)
		digits[index] = base58Alphabet[remainder.Int64()]
	}
	if value.Sign() != 0 {
		return "", errors.New("value does not fit its base58 width")
	}
	return string(digits), nil
}
