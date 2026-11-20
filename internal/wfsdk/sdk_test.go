package wfsdk

import (
	"fmt"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, version := range []string{Version(), fmt.Sprintf("%d.0", MinMajor)} {
		if err := Check(version); err != nil {
			t.Fatalf("expected %s supported, got %v", version, err)
		}
	}
	cases := map[string]string{
		fmt.Sprintf("%d.9", MinMajor-1):      "no longer supported",
		fmt.Sprintf("%d.0", Major+1):         "newer than",
		fmt.Sprintf("%d.%d", Major, Minor+1): "newer than",
		"1":                                  "MAJOR.MINOR",
		"x.y":                                "MAJOR.MINOR",
	}
	for version, want := range cases {
		err := Check(version)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "workflow SDK") {
			t.Fatalf("expected Check(%q) workflow error containing %q, got %v", version, want, err)
		}
	}
	if err := Check("0.1"); err == nil || !strings.Contains(err.Error(), "autablectl workflow sdk") {
		t.Fatalf("expected the refresh command in the error, got %v", err)
	}
}

func TestFilesEmbedTheSDK(t *testing.T) {
	index, ok := Files()["index.ts"]
	if !ok {
		t.Fatalf("expected embedded index.ts, got %v", Files())
	}
	if !strings.Contains(index, `SDK_VERSION = "`+Version()+`"`) {
		t.Fatalf("expected index.ts to declare SDK_VERSION = %q", Version())
	}
	for name, body := range Files() {
		if !strings.HasPrefix(body, "// autable workflow SDK.") {
			t.Fatalf("expected %s to start with the SDK marker comment", name)
		}
	}
}

func TestSDKDeclaresCryptoAndEncodings(t *testing.T) {
	files := Files()
	for name, wants := range map[string][]string{
		"host.ts": {
			`export type Encoding = "base64" | "base64url" | "hex" | "base58" | "utf8";`,
			`publicKey(privateKeyPem: string): string;`,
		},
		"crypto.ts": {
			`export type SignatureAlgorithm = "ed25519" | "ecdsa-p256-sha256" | "ecdsa-sha256-p1363" | "rsa-pkcs1-sha256";`,
			`publicKey(privateKeyPem: string): string {`,
			`export const base58: BinaryEncoding = {`,
			`host().encode("base58", toBytes(data))`,
			`host().decode("base58", text)`,
		},
		"index.ts": {
			`export { crypto, base64, base64url, hex, base58, utf8 } from "./crypto.js";`,
		},
	} {
		for _, want := range wants {
			if !strings.Contains(files[name], want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
}
