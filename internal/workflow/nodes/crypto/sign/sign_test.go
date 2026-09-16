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
	for key, want := range map[string]string{"algorithm": "ecdsa", "hash": "sha256", "format": "p1363", "encoding": "base64"} {
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
		"unsupported encoding":  {input: map[string]any{"data": "x", "encoding": "hex"}, secret: secret, message: `encoding "hex" is not supported, only base64`},
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
