package sign

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"

	"autable/internal/workflow"
)

func testKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, base64.StdEncoding.EncodeToString(der)
}

func infoWithKey(secret string) workflow.RuntimeInfo {
	return workflow.RuntimeInfo{Secrets: map[string]string{"private_key": secret}}
}

func TestNodeSignsWithP1363LayoutTheKeyVerifies(t *testing.T) {
	key, secret := testKey(t)

	output, err := NewNode().Run(context.Background(), map[string]any{"data": "PREFIX-1-ABC-20270101"}, infoWithKey(secret))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	signature, err := base64.StdEncoding.DecodeString(output["signature"].(string))
	if err != nil {
		t.Fatalf("signature is not base64: %v", err)
	}
	if len(signature) != 64 {
		t.Fatalf("P-256 p1363 signature must be 64 bytes, got %d", len(signature))
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	digest := sha256.Sum256([]byte("PREFIX-1-ABC-20270101"))
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Fatal("signature does not verify with the key's public key")
	}

	wantPublic, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if output["public_key"] != base64.StdEncoding.EncodeToString(wantPublic) {
		t.Fatalf("public_key = %v", output["public_key"])
	}
	for key, want := range map[string]string{"curve": "P-256", "algorithm": "ecdsa", "hash": "sha256", "format": "p1363", "encoding": "base64"} {
		if output[key] != want {
			t.Fatalf("output[%q] = %v, want %q", key, output[key], want)
		}
	}
}

func TestNodeAcceptsPEMAndExplicitDefaults(t *testing.T) {
	key, secret := testKey(t)
	der, _ := base64.StdEncoding.DecodeString(secret)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	output, err := NewNode().Run(context.Background(), map[string]any{
		"data":      "hello",
		"algorithm": " ECDSA ",
		"hash":      "SHA256",
		"format":    "P1363",
		"encoding":  "Base64",
	}, infoWithKey(pemKey))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	signature, _ := base64.StdEncoding.DecodeString(output["signature"].(string))
	digest := sha256.Sum256([]byte("hello"))
	if !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("PEM key signature does not verify")
	}
}

func TestNodeRejectsBadInput(t *testing.T) {
	_, secret := testKey(t)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaDER, _ := x509.MarshalPKCS8PrivateKey(rsaKey)

	for name, testCase := range map[string]struct {
		input   map[string]any
		secret  string
		message string
	}{
		"missing data":          {input: map[string]any{}, secret: secret, message: "data is required"},
		"empty data":            {input: map[string]any{"data": ""}, secret: secret, message: "data is required"},
		"missing key":           {input: map[string]any{"data": "x"}, secret: "", message: "private_key secret is required"},
		"key not base64":        {input: map[string]any{"data": "x"}, secret: "not*base64", message: "private_key is not base64"},
		"key not pkcs8":         {input: map[string]any{"data": "x"}, secret: base64.StdEncoding.EncodeToString([]byte("junk")), message: "not a PKCS#8 key"},
		"key not ecdsa":         {input: map[string]any{"data": "x"}, secret: base64.StdEncoding.EncodeToString(rsaDER), message: "only ECDSA keys are supported"},
		"unsupported algorithm": {input: map[string]any{"data": "x", "algorithm": "ed25519"}, secret: secret, message: `algorithm "ed25519" is not supported, only ecdsa`},
		"unsupported hash":      {input: map[string]any{"data": "x", "hash": "sha512"}, secret: secret, message: `hash "sha512" is not supported, only sha256`},
		"unsupported format":    {input: map[string]any{"data": "x", "format": "der"}, secret: secret, message: `format "der" is not supported, only p1363`},
		"unsupported encoding":  {input: map[string]any{"data": "x", "encoding": "hex"}, secret: secret, message: `encoding "hex" is not supported, only base64 or base58`},
		"option not a string":   {input: map[string]any{"data": "x", "format": 1}, secret: secret, message: "format must be a string"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewNode().Run(context.Background(), testCase.input, infoWithKey(testCase.secret))
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.message)
			}
		})
	}
}

func TestNodeInfoDocumentsBothLanguages(t *testing.T) {
	info := NewNode().Info()
	if info.Type != "crypto.sign" {
		t.Fatalf("type = %q", info.Type)
	}
	if len(info.Secrets) != 1 || info.Secrets[0].Name != "private_key" {
		t.Fatalf("secrets = %#v", info.Secrets)
	}
	for _, language := range []string{"en-US", "zh-CN"} {
		if strings.TrimSpace(info.Documentation[language]) == "" {
			t.Fatalf("documentation for %s is empty", language)
		}
	}
}

// A secp160r1 key pair generated with OpenSSL (node:crypto), as the licensing
// tools this curve is used with produce it. Test material only.
const (
	secp160r1TestKey = "MGECAQAwEAYHKoZIzj0CAQYFK4EEAAgESjBIAgEBBBUAGz2+8KXEI9UyPdGkVGdSaHWWQmqhLAMqAAQRVWwh7NHjIX5lKWnffcvhzSg8SsKxfCFdLs36C2IrroelNax/ZnjF"
	secp160r1TestPub = "MD4wEAYHKoZIzj0CAQYFK4EEAAgDKgAEEVVsIezR4yF+ZSlp333L4c0oPErCsXwhXS7N+gtiK66HpTWsf2Z4xQ=="
)

func TestNodeSignsOnSecp160r1WithFixedWidthBase58(t *testing.T) {
	output, err := NewNode().Run(context.Background(), map[string]any{"data": "PREFIX-1-ABC-20270101", "encoding": "base58"}, infoWithKey(secp160r1TestKey))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output["curve"] != "secp160r1" || output["encoding"] != "base58" {
		t.Fatalf("output = %#v", output)
	}
	// The public key derived from the scalar must be byte-for-byte what
	// OpenSSL wrote next to it, or the client's embedded key will not match.
	if output["public_key"] != secp160r1TestPub {
		t.Fatalf("public_key = %v, want the OpenSSL SPKI", output["public_key"])
	}

	code := output["signature"].(string)
	if len(code) != 58 {
		t.Fatalf("a 42-byte signature is 58 base58 digits, got %d: %q", len(code), code)
	}
	for _, r := range code {
		if !strings.ContainsRune(base58Alphabet, r) {
			t.Fatalf("signature uses a character outside the alphabet: %q", code)
		}
	}
	signature := decodeBase58ForTest(t, code, 42)
	key, curveName, err := parsePrivateKey(secp160r1TestKey)
	if err != nil || curveName != "secp160r1" {
		t.Fatalf("parse: %v (%s)", err, curveName)
	}
	digest := sha256.Sum256([]byte("PREFIX-1-ABC-20270101"))
	if !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(signature[:21]), new(big.Int).SetBytes(signature[21:])) {
		t.Fatal("signature does not verify on secp160r1")
	}
}

func TestSecp160r1GeneratorIsOnTheCurve(t *testing.T) {
	if !secp160r1.params.IsOnCurve(secp160r1.params.Gx, secp160r1.params.Gy) {
		t.Fatal("secp160r1 base point is not on the curve; a constant is wrong")
	}
	if got := (secp160r1.params.N.BitLen() + 7) / 8; got != 21 {
		t.Fatalf("secp160r1 order is 161 bits, so r and s are 21 bytes each; got %d", got)
	}
}

func TestFixedWidthBase58(t *testing.T) {
	if width := base58Width(42); width != 58 {
		t.Fatalf("42 bytes need 58 digits, got %d", width)
	}
	if width := base58Width(64); width != 88 {
		t.Fatalf("64 bytes need 88 digits, got %d", width)
	}
	zeros, err := encodeBase58Fixed(make([]byte, 42))
	if err != nil || zeros != strings.Repeat("1", 58) {
		t.Fatalf("all-zero value = %q, %v", zeros, err)
	}
	data, _ := hex.DecodeString("c8c9cacbcccdcecfd0d1d2d3d4d5d6d7d8d9dadbdcdddedfe0e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1")
	encoded, err := encodeBase58Fixed(data)
	if err != nil || encoded != "4MMwZzFfyt3bMfuvj5EKa4XxLjh2wHUDtZD7yy5WqhUQNpEiLJg7vDX4c8" {
		t.Fatalf("encoded = %q, %v; want the independently computed digits", encoded, err)
	}
}

func decodeBase58ForTest(t *testing.T, code string, size int) []byte {
	t.Helper()
	value := new(big.Int)
	for _, r := range code {
		value.Mul(value, big.NewInt(58))
		value.Add(value, big.NewInt(int64(strings.IndexRune(base58Alphabet, r))))
	}
	out := make([]byte, size)
	value.FillBytes(out)
	return out
}
