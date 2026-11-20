package wfruntime

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"

	"github.com/dop251/goja"
)

const maxRandomBytes = 65536

func hashConstructor(alg string) (func() hash.Hash, error) {
	switch alg {
	case "sha1":
		return sha1.New, nil
	case "sha256":
		return sha256.New, nil
	case "sha512":
		return sha512.New, nil
	case "md5":
		return md5.New, nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm %q (use sha1, sha256, sha512 or md5)", alg)
	}
}

func (machine *vm) hostHash(call goja.FunctionCall) goja.Value {
	newHash, err := hashConstructor(call.Argument(0).String())
	if err != nil {
		machine.throw(err)
	}
	digest := newHash()
	digest.Write(machine.bytesArg(call.Argument(1), "data", true))
	return machine.buffer(digest.Sum(nil))
}

func (machine *vm) hostHMAC(call goja.FunctionCall) goja.Value {
	newHash, err := hashConstructor(call.Argument(0).String())
	if err != nil {
		machine.throw(err)
	}
	mac := hmac.New(newHash, machine.bytesArg(call.Argument(1), "key", true))
	mac.Write(machine.bytesArg(call.Argument(2), "data", true))
	return machine.buffer(mac.Sum(nil))
}

func (machine *vm) hostRandomBytes(call goja.FunctionCall) goja.Value {
	size := call.Argument(0).ToInteger()
	if size < 0 || size > maxRandomBytes {
		machine.throw(fmt.Errorf("randomBytes size must be between 0 and %d, got %d", maxRandomBytes, size))
	}
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		machine.throw(err)
	}
	return machine.buffer(data)
}

func (machine *vm) hostTimingSafeEqual(call goja.FunctionCall) goja.Value {
	left := machine.bytesArg(call.Argument(0), "a", true)
	right := machine.bytesArg(call.Argument(1), "b", true)
	return machine.rt.ToValue(subtle.ConstantTimeCompare(left, right) == 1)
}

func (machine *vm) hostSign(call goja.FunctionCall) goja.Value {
	signature, err := Sign(call.Argument(0).String(), call.Argument(1).String(), machine.bytesArg(call.Argument(2), "data", true))
	if err != nil {
		machine.throw(err)
	}
	return machine.buffer(signature)
}

func (machine *vm) hostVerify(call goja.FunctionCall) goja.Value {
	ok, err := Verify(call.Argument(0).String(), call.Argument(1).String(), machine.bytesArg(call.Argument(2), "data", true), machine.bytesArg(call.Argument(3), "signature", false))
	if err != nil {
		machine.throw(err)
	}
	return machine.rt.ToValue(ok)
}

func (machine *vm) optionalBytes(value goja.Value, name string) []byte {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil
	}
	return machine.bytesArg(value, name, true)
}

func (machine *vm) hostEncrypt(call goja.FunctionCall) goja.Value {
	out, err := Encrypt(call.Argument(0).String(), machine.bytesArg(call.Argument(1), "key", false), machine.bytesArg(call.Argument(2), "iv", false), machine.bytesArg(call.Argument(3), "data", true), machine.optionalBytes(call.Argument(4), "aad"))
	if err != nil {
		machine.throw(err)
	}
	return machine.buffer(out)
}

func (machine *vm) hostDecrypt(call goja.FunctionCall) goja.Value {
	out, err := Decrypt(call.Argument(0).String(), machine.bytesArg(call.Argument(1), "key", false), machine.bytesArg(call.Argument(2), "iv", false), machine.bytesArg(call.Argument(3), "data", false), machine.optionalBytes(call.Argument(4), "aad"))
	if err != nil {
		machine.throw(err)
	}
	return machine.buffer(out)
}

func (machine *vm) hostEncode(call goja.FunctionCall) goja.Value {
	text, err := Encode(call.Argument(0).String(), machine.bytesArg(call.Argument(1), "bytes", false))
	if err != nil {
		machine.throw(err)
	}
	return machine.rt.ToValue(text)
}

func (machine *vm) hostDecode(call goja.FunctionCall) goja.Value {
	data, err := Decode(call.Argument(0).String(), call.Argument(1).String())
	if err != nil {
		machine.throw(err)
	}
	return machine.buffer(data)
}

// Encode renders bytes as base64, base64url (unpadded), hex or utf8.
func Encode(encoding string, data []byte) (string, error) {
	switch encoding {
	case "base64":
		return base64.StdEncoding.EncodeToString(data), nil
	case "base64url":
		return base64.RawURLEncoding.EncodeToString(data), nil
	case "hex":
		return hex.EncodeToString(data), nil
	case "utf8":
		return strings.ToValidUTF8(string(data), "�"), nil
	default:
		return "", fmt.Errorf("unsupported encoding %q (use base64, base64url, hex or utf8)", encoding)
	}
}

// Decode parses text in the encoding; base64 variants accept padded and
// unpadded input.
func Decode(encoding, text string) ([]byte, error) {
	var data []byte
	var err error
	switch encoding {
	case "base64":
		data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(text, "="))
	case "base64url":
		data, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(text, "="))
	case "hex":
		data, err = hex.DecodeString(text)
	case "utf8":
		data = []byte(text)
	default:
		return nil, fmt.Errorf("unsupported encoding %q (use base64, base64url, hex or utf8)", encoding)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid %s input: %w", encoding, err)
	}
	return data, nil
}

func pemBlock(text string) (*pem.Block, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(text)))
	if block == nil {
		return nil, errors.New("key is not PEM encoded")
	}
	return block, nil
}

func parsePrivateKey(text string) (crypto.PrivateKey, error) {
	block, err := pemBlock(text)
	if err != nil {
		return nil, err
	}
	switch block.Type {
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key PEM type %q", block.Type)
	}
}

func parsePublicKey(text string) (crypto.PublicKey, error) {
	block, err := pemBlock(text)
	if err != nil {
		return nil, err
	}
	switch block.Type {
	case "PUBLIC KEY":
		return x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	case "CERTIFICATE":
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		return certificate.PublicKey, nil
	default:
		return nil, fmt.Errorf("unsupported public key PEM type %q", block.Type)
	}
}

// Sign signs data with a PEM private key.
func Sign(alg, privateKeyPEM string, data []byte) ([]byte, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	switch alg {
	case "ed25519":
		private, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("ed25519 needs an Ed25519 private key")
		}
		return ed25519.Sign(private, data), nil
	case "ecdsa-p256-sha256":
		private, ok := key.(*ecdsa.PrivateKey)
		if !ok || private.Curve != elliptic.P256() {
			return nil, errors.New("ecdsa-p256-sha256 needs a P-256 private key")
		}
		digest := sha256.Sum256(data)
		return ecdsa.SignASN1(rand.Reader, private, digest[:])
	case "rsa-pkcs1-sha256":
		private, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("rsa-pkcs1-sha256 needs an RSA private key")
		}
		digest := sha256.Sum256(data)
		return rsa.SignPKCS1v15(nil, private, crypto.SHA256, digest[:])
	default:
		return nil, fmt.Errorf("unsupported signature algorithm %q (use ed25519, ecdsa-p256-sha256 or rsa-pkcs1-sha256)", alg)
	}
}

// Verify checks a signature with a PEM public key or certificate. A wrong
// signature is (false, nil); a bad key or algorithm is an error.
func Verify(alg, publicKeyPEM string, data, signature []byte) (bool, error) {
	key, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		return false, err
	}
	switch alg {
	case "ed25519":
		public, ok := key.(ed25519.PublicKey)
		if !ok {
			return false, errors.New("ed25519 needs an Ed25519 public key")
		}
		return ed25519.Verify(public, data, signature), nil
	case "ecdsa-p256-sha256":
		public, ok := key.(*ecdsa.PublicKey)
		if !ok || public.Curve != elliptic.P256() {
			return false, errors.New("ecdsa-p256-sha256 needs a P-256 public key")
		}
		digest := sha256.Sum256(data)
		if len(signature) == 64 {
			r := new(big.Int).SetBytes(signature[:32])
			s := new(big.Int).SetBytes(signature[32:])
			if ecdsa.Verify(public, digest[:], r, s) {
				return true, nil
			}
		}
		return ecdsa.VerifyASN1(public, digest[:], signature), nil
	case "rsa-pkcs1-sha256":
		public, ok := key.(*rsa.PublicKey)
		if !ok {
			return false, errors.New("rsa-pkcs1-sha256 needs an RSA public key")
		}
		digest := sha256.Sum256(data)
		return rsa.VerifyPKCS1v15(public, crypto.SHA256, digest[:], signature) == nil, nil
	default:
		return false, fmt.Errorf("unsupported signature algorithm %q (use ed25519, ecdsa-p256-sha256 or rsa-pkcs1-sha256)", alg)
	}
}

// Encrypt encrypts with aes-gcm (output: ciphertext followed by the tag)
// or aes-cbc (PKCS #7 padding).
func Encrypt(alg string, key, iv, data, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	switch alg {
	case "aes-gcm":
		gcm, err := newGCM(block, iv)
		if err != nil {
			return nil, err
		}
		return gcm.Seal(nil, iv, data, aad), nil
	case "aes-cbc":
		if aad != nil {
			return nil, errors.New("aes-cbc takes no additional data")
		}
		if len(iv) != aes.BlockSize {
			return nil, fmt.Errorf("aes-cbc needs a %d-byte iv, got %d", aes.BlockSize, len(iv))
		}
		padding := aes.BlockSize - len(data)%aes.BlockSize
		padded := append(append([]byte(nil), data...), bytes.Repeat([]byte{byte(padding)}, padding)...)
		out := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported cipher %q (use aes-gcm or aes-cbc)", alg)
	}
}

// Decrypt reverses Encrypt; authentication and padding failures are errors.
func Decrypt(alg string, key, iv, data, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	switch alg {
	case "aes-gcm":
		gcm, err := newGCM(block, iv)
		if err != nil {
			return nil, err
		}
		out, err := gcm.Open(nil, iv, data, aad)
		if err != nil {
			return nil, errors.New("aes-gcm: message authentication failed")
		}
		return out, nil
	case "aes-cbc":
		if aad != nil {
			return nil, errors.New("aes-cbc takes no additional data")
		}
		if len(iv) != aes.BlockSize {
			return nil, fmt.Errorf("aes-cbc needs a %d-byte iv, got %d", aes.BlockSize, len(iv))
		}
		if len(data) == 0 || len(data)%aes.BlockSize != 0 {
			return nil, errors.New("aes-cbc: ciphertext is not a multiple of the block size")
		}
		out := make([]byte, len(data))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
		padding := int(out[len(out)-1])
		if padding == 0 || padding > aes.BlockSize || padding > len(out) {
			return nil, errors.New("aes-cbc: invalid padding")
		}
		for _, value := range out[len(out)-padding:] {
			if int(value) != padding {
				return nil, errors.New("aes-cbc: invalid padding")
			}
		}
		return out[:len(out)-padding], nil
	default:
		return nil, fmt.Errorf("unsupported cipher %q (use aes-gcm or aes-cbc)", alg)
	}
}

func newGCM(block cipher.Block, iv []byte) (cipher.AEAD, error) {
	if len(iv) == 0 {
		return nil, errors.New("aes-gcm needs an iv")
	}
	if len(iv) == 12 {
		return cipher.NewGCM(block)
	}
	return cipher.NewGCMWithNonceSize(block, len(iv))
}
