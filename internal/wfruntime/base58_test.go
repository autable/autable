package wfruntime

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

func TestBase58Vectors(t *testing.T) {
	data42, _ := hex.DecodeString("c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedfe0e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1")
	for _, testCase := range []struct {
		data []byte
		want string
	}{
		{nil, ""},
		{[]byte{0}, "11"},
		{[]byte{255}, "5Q"},
		// Bitcoin base58 vectors whose shortest form already has the fixed width.
		{[]byte("Hello World!"), "2NEpo7TZRRrLZSi2U"},
		// One digit short of the width, so the fixed form gains a leading "1".
		{[]byte("The quick brown fox jumps over the lazy dog."), "1USm3fpXnKG5EUBx2ndxBDMPVciP5hGey2Jh4NDv6gmeo1LkMeiKrLJUUBk6Z"},
		{data42, "4MMwZzFfyt3bMfuvj5EKa4XxLjh2wHUDtZD7yy5WqhUQNpEiLJg7vDX4c8"},
		{make([]byte, 42), strings.Repeat("1", 58)},
	} {
		got, err := Encode("base58", testCase.data)
		if err != nil || got != testCase.want {
			t.Errorf("encode %x = %q, %v; want %q", testCase.data, got, err, testCase.want)
		}
		back, err := Decode("base58", testCase.want)
		if err != nil || !bytes.Equal(back, testCase.data) {
			t.Errorf("decode %q = %x, %v; want %x", testCase.want, back, err, testCase.data)
		}
	}
}

func TestBase58FixedWidth(t *testing.T) {
	for byteLength, want := range map[int]int{0: 0, 1: 2, 2: 3, 3: 5, 20: 28, 32: 44, 42: 58, 64: 88} {
		if got := base58Width(byteLength); got != want {
			t.Errorf("%d bytes: width %d, want %d", byteLength, got, want)
		}
	}
	// The width is the smallest w with 58^w >= 256^n, checked exactly.
	radix, power, limit := big.NewInt(58), big.NewInt(1), big.NewInt(1)
	digits := 0
	for byteLength := 0; byteLength <= maxBase58Bytes; byteLength++ {
		if byteLength > 0 {
			limit.Lsh(limit, 8)
		}
		for power.Cmp(limit) < 0 {
			power.Mul(power, radix)
			digits++
		}
		if got := base58Width(byteLength); got != digits {
			t.Fatalf("%d bytes: width %d, exact %d", byteLength, got, digits)
		}
	}
	for byteLength := 0; byteLength <= 80; byteLength++ {
		data := make([]byte, byteLength)
		rand.Read(data)
		if byteLength > 1 {
			data[0] = 0 // leading zeros must not shorten or lengthen the code
		}
		for _, sample := range [][]byte{data, make([]byte, byteLength), bytes.Repeat([]byte{255}, byteLength)} {
			code, err := Encode("base58", sample)
			if err != nil || len(code) != base58Width(byteLength) {
				t.Fatalf("%x: %q, %v; want %d digits", sample, code, err, base58Width(byteLength))
			}
			back, err := Decode("base58", code)
			if err != nil || !bytes.Equal(back, sample) {
				t.Fatalf("%q decoded to %x, %v; want %x", code, back, err, sample)
			}
		}
	}
}

func TestBase58Rejects(t *testing.T) {
	for text, want := range map[string]string{
		"1O":                        `'O' at offset 1 is not a base58 digit`,
		"0":                         `'0' at offset 0 is not a base58 digit`,
		"名称":                        `'名' at offset 0 is not a base58 digit`,
		"1111":                      "4 digits is not the fixed width of any byte length",
		"zz":                        "value does not fit the byte length of its width",
		strings.Repeat("z", 58):     "value does not fit the byte length of its width",
		strings.Repeat("1", 100000): "100000 digits is not the fixed width of any byte length up to 1024",
	} {
		if _, err := Decode("base58", text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("decode %.12q: error %v, want %q", text, err, want)
		}
	}
	if _, err := Encode("base58", make([]byte, maxBase58Bytes+1)); err == nil || !strings.Contains(err.Error(), "at most 1024 bytes") {
		t.Fatalf("expected the size cap, got %v", err)
	}
}

func TestBase58ThroughHost(t *testing.T) {
	output := cryptoRun(t, `return {
		code: h.encode("base58", h.decode("utf8", "Hello World!")),
		back: h.encode("utf8", h.decode("base58", "2NEpo7TZRRrLZSi2U")),
		width: h.encode("base58", new Uint8Array(42)).length,
	};`, nil)
	expectValues(t, output, map[string]any{"code": "2NEpo7TZRRrLZSi2U", "back": "Hello World!", "width": 58.0})
}
