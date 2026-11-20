package wfruntime

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"maps"
	"regexp"
	"slices"
	"testing"

	"autable/internal/wfsdk"
)

func pemOf(t *testing.T, kind string, der []byte) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
}

// cryptoRun evaluates expressions with a few helpers and returns their
// values by name.
func cryptoRun(t *testing.T, body string, secrets map[string]string) map[string]any {
	t.Helper()
	result := mustRun(t, bundle(`function () {
		const h = __autable_host;
		const hex = (buffer) => h.encode("hex", buffer);
		const unhex = (text) => h.decode("hex", text);
		const secret = (name) => h.secret(name);
		`+body+`
	}`), Options{Secrets: secrets})
	var output map[string]any
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("invalid output %q: %v", result.Output, err)
	}
	return output
}

func expectValues(t *testing.T, output map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if output[key] != value {
			t.Errorf("%s: got %#v, want %#v", key, output[key], value)
		}
	}
}

func TestHashAndHMACVectors(t *testing.T) {
	output := cryptoRun(t, `return {
		sha1: hex(h.hash("sha1", "abc")),
		sha256: hex(h.hash("sha256", new TextEncoder().encode("abc"))),
		sha512: hex(h.hash("sha512", "abc")),
		md5: hex(h.hash("md5", "abc")),
		hmac256: hex(h.hmac("sha256", "Jefe", "what do ya want for nothing?")),
		hmac1: hex(h.hmac("sha1", "Jefe", "what do ya want for nothing?")),
		hmacKeyBytes: hex(h.hmac("sha256", unhex("0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b"), "Hi There")),
		random: h.randomBytes(16).byteLength,
		equal: h.timingSafeEqual("abc", new TextEncoder().encode("abc")),
		unequal: h.timingSafeEqual("abc", "abd"),
		b64: h.encode("base64", unhex("fbff")),
		b64url: h.encode("base64url", unhex("fbff")),
		fromB64: hex(h.decode("base64", "+/8")),
		fromB64url: hex(h.decode("base64url", "-_8=")),
		utf8: h.encode("utf8", h.decode("utf8", "名称")),
	};`, nil)
	expectValues(t, output, map[string]any{
		"sha1":         "a9993e364706816aba3e25717850c26c9cd0d89d",
		"sha256":       "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"sha512":       "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f",
		"md5":          "900150983cd24fb0d6963f7d28e17f72",
		"hmac256":      "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843", // RFC 4231 case 2
		"hmac1":        "effcdf6ae5eb2fa2d27416d5f184df9c259a7c79",                         // RFC 2202 case 2
		"hmacKeyBytes": "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7", // RFC 4231 case 1
		"random":       16.0,
		"equal":        true,
		"unequal":      false,
		"b64":          "+/8=",
		"b64url":       "-_8",
		"fromB64":      "fbff",
		"fromB64url":   "fbff",
		"utf8":         "名称",
	})
}

func TestEd25519Vector(t *testing.T) {
	// RFC 8032, section 7.1, test 1 (empty message).
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	private := ed25519.NewKeyFromSeed(seed)
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	output := cryptoRun(t, `
		const signature = h.sign("ed25519", secret("private"), new Uint8Array(0));
		return {
			signature: hex(signature),
			valid: h.verify("ed25519", secret("public"), new Uint8Array(0), signature),
			tampered: h.verify("ed25519", secret("public"), "x", signature),
		};`, map[string]string{"private": pemOf(t, "PRIVATE KEY", privateDER), "public": pemOf(t, "PUBLIC KEY", publicDER)})
	expectValues(t, output, map[string]any{
		"signature": "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b",
		"valid":     true,
		"tampered":  false,
	})
}

func TestECDSAAndRSASignatures(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPrivateDER, _ := x509.MarshalECPrivateKey(ecKey)
	ecPublicDER, _ := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPrivateDER, _ := x509.MarshalPKCS8PrivateKey(rsaKey)
	// An independent signature made in Go, raw r||s form for ECDSA.
	digest := sha256.Sum256([]byte("payload"))
	r, s, err := ecdsa.Sign(rand.Reader, ecKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	rsaSignature, err := rsa.SignPKCS1v15(nil, rsaKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	output := cryptoRun(t, `
		const ec = h.sign("ecdsa-p256-sha256", secret("ecPrivate"), "payload");
		const rsaSig = h.sign("rsa-pkcs1-sha256", secret("rsaPrivate"), "payload");
		let wrongKey = "";
		try { h.sign("ed25519", secret("ecPrivate"), "payload"); } catch (error) { wrongKey = error.message; }
		return {
			ecDER: hex(ec),
			ecValid: h.verify("ecdsa-p256-sha256", secret("ecPublic"), "payload", ec),
			ecRaw: h.verify("ecdsa-p256-sha256", secret("ecPublic"), "payload", unhex(secret("ecRaw"))),
			ecWrong: h.verify("ecdsa-p256-sha256", secret("ecPublic"), "other", ec),
			rsaSig: hex(rsaSig),
			rsaValid: h.verify("rsa-pkcs1-sha256", secret("rsaPublic"), "payload", unhex(secret("rsaGo"))),
			wrongKey,
		};`, map[string]string{
		"ecPrivate":  pemOf(t, "EC PRIVATE KEY", ecPrivateDER),
		"ecPublic":   pemOf(t, "PUBLIC KEY", ecPublicDER),
		"ecRaw":      hex.EncodeToString(raw),
		"rsaPrivate": pemOf(t, "PRIVATE KEY", rsaPrivateDER),
		"rsaPublic":  pemOf(t, "RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(&rsaKey.PublicKey)),
		"rsaGo":      hex.EncodeToString(rsaSignature),
	})
	expectValues(t, output, map[string]any{"ecValid": true, "ecRaw": true, "ecWrong": false, "rsaValid": true, "wrongKey": "ed25519 needs an Ed25519 private key"})
	der, _ := hex.DecodeString(output["ecDER"].(string))
	if !ecdsa.VerifyASN1(&ecKey.PublicKey, digest[:], der) {
		t.Fatal("expected the JavaScript ECDSA signature to verify in Go")
	}
	rsaSig, _ := hex.DecodeString(output["rsaSig"].(string))
	if rsa.VerifyPKCS1v15(&rsaKey.PublicKey, crypto.SHA256, digest[:], rsaSig) != nil {
		t.Fatal("expected the JavaScript RSA signature to verify in Go")
	}
}

func TestAESVectors(t *testing.T) {
	output := cryptoRun(t, `
		// "The Galois/Counter Mode of Operation", test case 3.
		const key = unhex("feffe9928665731c6d6a8f9467308308");
		const iv = unhex("cafebabefacedbaddecaf888");
		const plain = unhex("d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532fcf0e2449a6b525b16aedf5aa0de657ba637b391aafd255");
		const sealed = h.encrypt("aes-gcm", key, iv, plain);
		const tampered = new Uint8Array(sealed.slice(0));
		tampered[0] ^= 1;
		let authError = "";
		try { h.decrypt("aes-gcm", key, iv, tampered); } catch (error) { authError = error.message; }
		// NIST SP 800-38A F.2.1, first block, plus a PKCS #7 padding block.
		const cbcKey = unhex("2b7e151628aed2a6abf7158809cf4f3c");
		const cbcIV = unhex("000102030405060708090a0b0c0d0e0f");
		const cbc = h.encrypt("aes-cbc", cbcKey, cbcIV, unhex("6bc1bee22e409f96e93d7e117393172a"));
		const withAAD = h.encrypt("aes-gcm", key, iv, "hello", "context");
		let aadError = "";
		try { h.decrypt("aes-gcm", key, iv, withAAD, "other"); } catch (error) { aadError = error.message; }
		return {
			gcm: hex(sealed),
			gcmOpen: hex(h.decrypt("aes-gcm", key, iv, sealed)),
			authError,
			cbcFirst: hex(cbc).slice(0, 32),
			cbcLength: cbc.byteLength,
			cbcOpen: hex(h.decrypt("aes-cbc", cbcKey, cbcIV, cbc)),
			aadOpen: new TextDecoder().decode(h.decrypt("aes-gcm", key, iv, withAAD, "context")),
			aadError,
		};`, nil)
	expectValues(t, output, map[string]any{
		"gcm":       "42831ec2217774244b7221b784d0d49ce3aa212f2c02a4e035c17e2329aca12e21d514b25466931c7d8f6a5aac84aa051ba30b396a0aac973d58e091473f5985" + "4d5c2af327cd64a62cf35abd2ba6fab4",
		"gcmOpen":   "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532fcf0e2449a6b525b16aedf5aa0de657ba637b391aafd255",
		"authError": "aes-gcm: message authentication failed",
		"cbcFirst":  "7649abac8119b246cee98e9b12e9197d",
		"cbcLength": 32.0,
		"cbcOpen":   "6bc1bee22e409f96e93d7e117393172a",
		"aadOpen":   "hello",
		"aadError":  "aes-gcm: message authentication failed",
	})
}

// Every host function the vendored SDK calls must exist on the host object.
func TestSDKCallsOnlyExistingHostFunctions(t *testing.T) {
	called := map[string]bool{}
	for _, body := range wfsdk.Files() {
		for _, match := range regexp.MustCompile(`host\(\)\.(\w+)\(`).FindAllStringSubmatch(body, -1) {
			called[match[1]] = true
		}
	}
	if !called["publicKey"] || !called["encode"] {
		t.Fatalf("expected the SDK to call publicKey and encode, got %v", called)
	}
	names, _ := json.Marshal(slices.Sorted(maps.Keys(called)))
	output := cryptoRun(t, `return { missing: `+string(names)+`.filter((name) => typeof h[name] !== "function").join(",") };`, nil)
	expectValues(t, output, map[string]any{"missing": ""})
}
