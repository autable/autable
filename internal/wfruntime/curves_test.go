package wfruntime

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

// A secp160r1 key pair generated with OpenSSL, PKCS #8 and SPKI as base64
// DER. Test material only.
const (
	secp160r1TestKey = "MGECAQAwEAYHKoZIzj0CAQYFK4EEAAgESjBIAgEBBBUAGz2+8KXEI9UyPdGkVGdSaHWWQmqhLAMqAAQRVWwh7NHjIX5lKWnffcvhzSg8SsKxfCFdLs36C2IrroelNax/ZnjF"
	secp160r1TestPub = "MD4wEAYHKoZIzj0CAQYFK4EEAAgDKgAEEVVsIezR4yF+ZSlp333L4c0oPErCsXwhXS7N+gtiK66HpTWsf2Z4xQ=="
)

func mustDecodeBase64(t *testing.T, text string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// verifyRaw checks an r||s signature in Go, split at half its length.
func verifyRaw(public *ecdsa.PublicKey, data string, signature []byte) bool {
	digest := sha256.Sum256([]byte(data))
	half := len(signature) / 2
	return ecdsa.Verify(public, digest[:], new(big.Int).SetBytes(signature[:half]), new(big.Int).SetBytes(signature[half:]))
}

func TestSecp160r1Parameters(t *testing.T) {
	params := secp160r1.params
	if !params.IsOnCurve(params.Gx, params.Gy) {
		t.Fatal("secp160r1 base point is not on the curve; a constant is wrong")
	}
	if x, _ := params.ScalarBaseMult(params.N.Bytes()); x.Sign() != 0 {
		t.Fatal("N times the base point is not the point at infinity; the order is wrong")
	}
	if got := orderBytes(params); got != 21 {
		t.Fatalf("secp160r1's order is 161 bits, so r and s are 21 bytes each; got %d", got)
	}
}

func TestP1363OnSecp160r1WithFixedWidthBase58(t *testing.T) {
	output := cryptoRun(t, `
		const data = "ORDER-1-ABC-20270101";
		const signature = h.sign("ecdsa-sha256-p1363", secret("private"), data);
		const code = h.encode("base58", signature);
		const back = h.decode("base58", code);
		const tampered = new Uint8Array(back.slice(0));
		tampered[41] ^= 1;
		return {
			signature: hex(signature),
			code,
			roundTrip: hex(back) === hex(signature),
			publicKey: h.publicKey(secret("private")),
			publicKeyFromPEM: h.publicKey(secret("privatePEM")),
			valid: h.verify("ecdsa-sha256-p1363", h.publicKey(secret("private")), data, back),
			validPEM: h.verify("ecdsa-sha256-p1363", secret("publicPEM"), data, signature),
			otherData: h.verify("ecdsa-sha256-p1363", secret("public"), "ORDER-2-ABC-20270101", signature),
			tampered: h.verify("ecdsa-sha256-p1363", secret("public"), data, tampered),
			short: h.verify("ecdsa-sha256-p1363", secret("public"), data, back.slice(1)),
		};`, map[string]string{
		"private":    secp160r1TestKey,
		"privatePEM": pemOf(t, "PRIVATE KEY", mustDecodeBase64(t, secp160r1TestKey)),
		"public":     secp160r1TestPub,
		"publicPEM":  pemOf(t, "PUBLIC KEY", mustDecodeBase64(t, secp160r1TestPub)),
	})
	// The public key derived from the scalar must be byte for byte what
	// OpenSSL wrote next to it, or a client's embedded key will not match.
	expectValues(t, output, map[string]any{
		"roundTrip":        true,
		"publicKey":        secp160r1TestPub,
		"publicKeyFromPEM": secp160r1TestPub,
		"valid":            true,
		"validPEM":         true,
		"otherData":        false,
		"tampered":         false,
		"short":            false,
	})
	code := output["code"].(string)
	if len(code) != 58 || strings.Trim(code, base58Alphabet) != "" {
		t.Fatalf("a 42-byte signature is 58 base58 digits, got %q", code)
	}
	signature, _ := hex.DecodeString(output["signature"].(string))
	if len(signature) != 42 {
		t.Fatalf("secp160r1 p1363 signature must be 42 bytes, got %d", len(signature))
	}
	key, err := parsePrivateKey(secp160r1TestKey)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyRaw(&key.(*ecdsa.PrivateKey).PublicKey, "ORDER-1-ABC-20270101", signature) {
		t.Fatal("signature does not verify in Go on secp160r1 with 21-byte halves")
	}
}

func TestP1363OnNISTCurves(t *testing.T) {
	for _, testCase := range []struct {
		curve elliptic.Curve
		size  int
	}{{elliptic.P256(), 64}, {elliptic.P384(), 96}, {elliptic.P521(), 132}} {
		name := testCase.curve.Params().Name
		t.Run(name, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(testCase.curve, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			privateDER, _ := x509.MarshalPKCS8PrivateKey(key)
			sec1DER, _ := x509.MarshalECPrivateKey(key)
			publicDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
			// What .NET's ECDsa.SignData returns by default: IEEE P1363,
			// each half padded to the order's byte length.
			digest := sha256.Sum256([]byte("payload"))
			r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			half := testCase.size / 2
			independent := make([]byte, testCase.size)
			r.FillBytes(independent[:half])
			s.FillBytes(independent[half:])
			der, _ := ecdsa.SignASN1(rand.Reader, key, digest[:])

			output := cryptoRun(t, `
				const signature = h.sign("ecdsa-sha256-p1363", secret("private"), "payload");
				const fromSEC1 = h.sign("ecdsa-sha256-p1363", secret("sec1"), "payload");
				return {
					signature: hex(signature),
					sec1Length: fromSEC1.byteLength,
					publicKey: h.publicKey(secret("private")),
					valid: h.verify("ecdsa-sha256-p1363", secret("public"), "payload", signature),
					independent: h.verify("ecdsa-sha256-p1363", secret("public"), "payload", unhex(secret("independent"))),
					der: h.verify("ecdsa-sha256-p1363", secret("public"), "payload", unhex(secret("der"))),
				};`, map[string]string{
				"private":     pemOf(t, "PRIVATE KEY", privateDER),
				"sec1":        pemOf(t, "EC PRIVATE KEY", sec1DER),
				"public":      base64.StdEncoding.EncodeToString(publicDER),
				"independent": hex.EncodeToString(independent),
				"der":         hex.EncodeToString(der),
			})
			expectValues(t, output, map[string]any{
				"sec1Length":  float64(testCase.size),
				"publicKey":   base64.StdEncoding.EncodeToString(publicDER),
				"valid":       true,
				"independent": true,
				"der":         false,
			})
			signature, _ := hex.DecodeString(output["signature"].(string))
			if len(signature) != testCase.size {
				t.Fatalf("%s p1363 signature must be %d bytes, got %d", name, testCase.size, len(signature))
			}
			if !verifyRaw(&key.PublicKey, "payload", signature) {
				t.Fatal("signature does not verify in Go as r||s")
			}
		})
	}
}

func TestP1363RejectsOtherKeysAndPublicKeyOfAnyKey(t *testing.T) {
	_, edPrivate, _ := ed25519.GenerateKey(rand.Reader)
	edDER, _ := x509.MarshalPKCS8PrivateKey(edPrivate)
	edPublic, _ := x509.MarshalPKIXPublicKey(edPrivate.Public())
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaDER := x509.MarshalPKCS1PrivateKey(rsaKey)
	rsaPublic, _ := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	output := cryptoRun(t, `
		const failure = (fn) => { try { fn(); return ""; } catch (error) { return error.message; } };
		return {
			edSign: failure(() => h.sign("ecdsa-sha256-p1363", secret("ed"), "x")),
			rsaVerify: failure(() => h.verify("ecdsa-sha256-p1363", secret("rsaPublic"), "x", new Uint8Array(42))),
			p256Only: failure(() => h.sign("ecdsa-p256-sha256", secret("secp"), "x")),
			badKey: failure(() => h.publicKey("not a key")),
			badAlgorithm: failure(() => h.sign("ecdsa", secret("secp"), "x")),
			edPublic: h.publicKey(secret("ed")),
			rsaPublic: h.publicKey(secret("rsa")),
		};`, map[string]string{
		"ed":        base64.StdEncoding.EncodeToString(edDER),
		"rsa":       pemOf(t, "RSA PRIVATE KEY", rsaDER),
		"rsaPublic": pemOf(t, "PUBLIC KEY", rsaPublic),
		"secp":      secp160r1TestKey,
	})
	expectValues(t, output, map[string]any{
		"edSign":       "ecdsa-sha256-p1363 needs an ECDSA private key",
		"rsaVerify":    "ecdsa-sha256-p1363 needs an ECDSA public key",
		"p256Only":     "ecdsa-p256-sha256 needs a P-256 private key",
		"badKey":       "key is neither PEM nor base64 DER",
		"badAlgorithm": `unsupported signature algorithm "ecdsa" (use ` + signatureAlgorithms + `)`,
		"edPublic":     base64.StdEncoding.EncodeToString(edPublic),
		"rsaPublic":    base64.StdEncoding.EncodeToString(rsaPublic),
	})
}

func TestCustomCurveKeyValidation(t *testing.T) {
	der := mustDecodeBase64(t, secp160r1TestKey)
	// Flip a bit of the embedded public key (the last byte of the DER).
	mismatched := append([]byte(nil), der...)
	mismatched[len(mismatched)-1] ^= 1
	if _, err := parsePrivateKey(base64.StdEncoding.EncodeToString(mismatched)); err == nil || !strings.Contains(err.Error(), "does not match its scalar") {
		t.Fatalf("expected a mismatched embedded public key to be rejected, got %v", err)
	}
	public := mustDecodeBase64(t, secp160r1TestPub)
	offCurve := append([]byte(nil), public...)
	offCurve[len(offCurve)-1] ^= 1
	if _, err := parsePublicKey(base64.StdEncoding.EncodeToString(offCurve)); err == nil || !strings.Contains(err.Error(), "not a valid secp160r1 point") {
		t.Fatalf("expected a point off the curve to be rejected, got %v", err)
	}
}
