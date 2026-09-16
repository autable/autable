package sign

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
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
	encodingBase58 = "base58"
)

type Node struct{}

func NewNode() Node {
	return Node{}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "crypto.sign",
		DisplayName:   "Sign data",
		Description:   "Signs a string with an ECDSA private key held as a secret and returns the encoded signature.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "data", Type: "string", Description: "The text to sign; its UTF-8 bytes are what gets signed."},
			{Name: "algorithm", Type: "string", Description: "Optional signature algorithm; only ecdsa is supported and it is the default."},
			{Name: "hash", Type: "string", Description: "Optional digest; only sha256 is supported and it is the default."},
			{Name: "format", Type: "string", Description: "Optional signature layout; only p1363 (raw r||s) is supported and it is the default."},
			{Name: "encoding", Type: "string", Description: "Optional output encoding: base64 (default) or base58, which is fixed-width and uses the Bitcoin alphabet."},
		},
		Outputs: []workflow.Port{
			{Name: "signature", Type: "string", Description: "The encoded signature."},
			{Name: "public_key", Type: "string", Description: "The public key matching the private key, as base64 SPKI, so a script can check which key signed."},
			{Name: "curve", Type: "string", Description: "The curve the key lives on, e.g. P-256 or secp160r1."},
			{Name: "algorithm", Type: "string", Description: "The algorithm used."},
			{Name: "hash", Type: "string", Description: "The digest used."},
			{Name: "format", Type: "string", Description: "The signature layout used."},
			{Name: "encoding", Type: "string", Description: "The output encoding used."},
		},
		Secrets: []workflow.Port{
			{Name: "private_key", Type: "string", Description: "PKCS#8 ECDSA private key, as base64 DER or PEM, on P-256, P-384, P-521 or secp160r1."},
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
	encoding, err := choiceInput(input, "encoding", encodingBase64, encodingBase58)
	if err != nil {
		return nil, err
	}

	key, curveName, err := parsePrivateKey(info.Secrets["private_key"])
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

	// IEEE P1363 sizes r and s by the group order, not the field: on
	// secp160r1 the order is 161 bits, so each half is 21 bytes.
	size := (key.Curve.Params().N.BitLen() + 7) / 8
	signature := make([]byte, 2*size)
	r.FillBytes(signature[:size])
	s.FillBytes(signature[size:])

	encoded := base64.StdEncoding.EncodeToString(signature)
	if encoding == encodingBase58 {
		encoded, err = encodeBase58Fixed(signature)
		if err != nil {
			return nil, err
		}
	}

	publicKey, err := marshalSPKI(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	return map[string]any{
		"signature":  encoded,
		"public_key": base64.StdEncoding.EncodeToString(publicKey),
		"curve":      curveName,
		"algorithm":  algorithm,
		"hash":       hash,
		"format":     format,
		"encoding":   encoding,
	}, nil
}

// choiceInput reads a parameter with a small set of supported values: absent
// or blank means the first one, anything else is rejected by name.
func choiceInput(input map[string]any, key string, supported ...string) (string, error) {
	raw, ok := input[key]
	if !ok || raw == nil {
		return supported[0], nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, raw)
	}
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return supported[0], nil
	}
	for _, candidate := range supported {
		if text == candidate {
			return text, nil
		}
	}
	return "", fmt.Errorf("%s %q is not supported, only %s", key, text, strings.Join(supported, " or "))
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

func parsePrivateKey(secret string) (*ecdsa.PrivateKey, string, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, "", errors.New("private_key secret is required")
	}
	var der []byte
	if strings.HasPrefix(secret, "-----BEGIN") {
		block, _ := pem.Decode([]byte(secret))
		if block == nil {
			return nil, "", errors.New("private_key is not valid PEM")
		}
		der = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(secret), ""))
		if err != nil {
			return nil, "", fmt.Errorf("private_key is not base64: %w", err)
		}
		der = decoded
	}
	return parsePKCS8ECDSA(der)
}

var _ workflow.Node = Node{}
