package sign

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"autable/internal/workflow"
)

const (
	algorithmECDSA = "ecdsa"
	hashSHA256     = "sha256"
	formatP1363    = "p1363"
	encodingBase64 = "base64"
)

type Node struct{}

func NewNode() Node {
	return Node{}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "crypto.sign",
		DisplayName:   "Sign data",
		Description:   "Signs a string with a private key held as a secret and returns the encoded signature.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "data", Type: "string", Description: "The text to sign; its UTF-8 bytes are what gets signed."},
			{Name: "algorithm", Type: "string", Description: "Optional signature algorithm; only ecdsa is supported and it is the default."},
			{Name: "hash", Type: "string", Description: "Optional digest; only sha256 is supported and it is the default."},
			{Name: "format", Type: "string", Description: "Optional signature layout; only p1363 (raw r||s) is supported and it is the default."},
			{Name: "encoding", Type: "string", Description: "Optional output encoding; only base64 is supported and it is the default."},
		},
		Outputs: []workflow.Port{
			{Name: "signature", Type: "string", Description: "The encoded signature."},
			{Name: "public_key", Type: "string", Description: "The public key matching the private key, as base64 SPKI, so a script can check which key signed."},
			{Name: "algorithm", Type: "string", Description: "The algorithm used."},
			{Name: "hash", Type: "string", Description: "The digest used."},
			{Name: "format", Type: "string", Description: "The signature layout used."},
			{Name: "encoding", Type: "string", Description: "The output encoding used."},
		},
		Secrets: []workflow.Port{
			{Name: "private_key", Type: "string", Description: "PKCS#8 private key, as base64 DER or PEM."},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := input["data"].(string)
	if !ok || data == "" {
		return nil, errors.New("data is required")
	}
	algorithm, err := optionInput(input, "algorithm", algorithmECDSA)
	if err != nil {
		return nil, err
	}
	hash, err := optionInput(input, "hash", hashSHA256)
	if err != nil {
		return nil, err
	}
	format, err := optionInput(input, "format", formatP1363)
	if err != nil {
		return nil, err
	}
	encoding, err := optionInput(input, "encoding", encodingBase64)
	if err != nil {
		return nil, err
	}

	key, err := parsePrivateKey(info.Secrets["private_key"])
	if err != nil {
		return nil, err
	}

	digest := sha256.Sum256([]byte(data))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("ecdsa sign: %w", err)
	}
	// Refuse to hand out a signature the matching public key does not accept:
	// a mismatched key or parameter set should fail here, not at the verifier.
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		return nil, errors.New("signature did not verify against the key's own public key")
	}

	size := (key.Curve.Params().BitSize + 7) / 8
	signature := make([]byte, 2*size)
	r.FillBytes(signature[:size])
	s.FillBytes(signature[size:])

	publicKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	return map[string]any{
		"signature":  base64.StdEncoding.EncodeToString(signature),
		"public_key": base64.StdEncoding.EncodeToString(publicKey),
		"algorithm":  algorithm,
		"hash":       hash,
		"format":     format,
		"encoding":   encoding,
	}, nil
}

// optionInput reads a parameter that has exactly one supported value: absent
// or blank means that value, anything else is rejected by name so a script
// asking for an unimplemented variant fails loudly instead of silently
// getting the default.
func optionInput(input map[string]any, key string, supported string) (string, error) {
	raw, ok := input[key]
	if !ok || raw == nil {
		return supported, nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, raw)
	}
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return supported, nil
	}
	if text != supported {
		return "", fmt.Errorf("%s %q is not supported, only %s", key, text, supported)
	}
	return text, nil
}

func parsePrivateKey(secret string) (*ecdsa.PrivateKey, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, errors.New("private_key secret is required")
	}
	var der []byte
	if strings.HasPrefix(secret, "-----BEGIN") {
		block, _ := pem.Decode([]byte(secret))
		if block == nil {
			return nil, errors.New("private_key is not valid PEM")
		}
		der = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(secret), ""))
		if err != nil {
			return nil, fmt.Errorf("private_key is not base64: %w", err)
		}
		der = decoded
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("private_key is not a PKCS#8 key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private_key is a %T, only ECDSA keys are supported", parsed)
	}
	return key, nil
}

var _ workflow.Node = Node{}
