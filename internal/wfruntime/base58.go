package wfruntime

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// The Bitcoin alphabet: digits and letters without 0, O, I and l, so a code
// read aloud or typed by hand has no look-alike characters.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// maxBase58Bytes bounds base58 input: the conversion is quadratic, and the
// encoding exists for short codes.
const maxBase58Bytes = 1024

// base58Width is how many base58 digits a value of the given byte length
// always fits in: ceil(bits / log2(58)).
func base58Width(byteLength int) int {
	return int(math.Ceil(float64(8*byteLength) / math.Log2(58)))
}

// encodeBase58 writes the bytes as one big-endian integer in exactly
// base58Width(len(data)) digits, left-padded with "1" (the digit zero), so
// the same number of bytes always gives the same number of characters. This
// is not Bitcoin's leading-zero rule (one "1" per leading zero byte, then
// the shortest digits).
func encodeBase58(data []byte) (string, error) {
	if len(data) > maxBase58Bytes {
		return "", fmt.Errorf("base58 encodes at most %d bytes, got %d", maxBase58Bytes, len(data))
	}
	width := base58Width(len(data))
	value := new(big.Int).SetBytes(data)
	digits := make([]byte, width)
	radix := big.NewInt(58)
	remainder := new(big.Int)
	for index := width - 1; index >= 0; index-- {
		value.QuoRem(value, radix, remainder)
		digits[index] = base58Alphabet[remainder.Int64()]
	}
	return string(digits), nil
}

// decodeBase58 reverses encodeBase58: the text's length selects the byte
// length whose width it is, and any other length is an error.
func decodeBase58(text string) ([]byte, error) {
	value := new(big.Int)
	radix := big.NewInt(58)
	digit := new(big.Int)
	for offset, char := range text {
		index := strings.IndexRune(base58Alphabet, char)
		if index < 0 {
			return nil, fmt.Errorf("%q at offset %d is not a base58 digit", char, offset)
		}
		value.Mul(value, radix)
		value.Add(value, digit.SetInt64(int64(index)))
	}
	size := -1
	for byteLength := 0; byteLength <= maxBase58Bytes; byteLength++ {
		width := base58Width(byteLength)
		if width == len(text) {
			size = byteLength
			break
		}
		if width > len(text) {
			break
		}
	}
	if size < 0 {
		return nil, fmt.Errorf("%d digits is not the fixed width of any byte length up to %d", len(text), maxBase58Bytes)
	}
	if value.BitLen() > 8*size {
		return nil, errors.New("value does not fit the byte length of its width")
	}
	data := make([]byte, size)
	value.FillBytes(data)
	return data, nil
}
