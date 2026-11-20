package wfruntime

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// Go's x509 only understands the NIST curves, so PKCS #8, SEC 1 and SPKI
// keys are first checked for a curve listed in customCurves, which are
// built from their parameters; everything else goes through x509. ECDSA on
// a custom curve uses crypto/ecdsa's generic big.Int implementation, which
// is not constant-time: acceptable for a server issuing short codes, not
// for keys an attacker can time at will.

var (
	oidECPublicKey = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidSecp160r1   = asn1.ObjectIdentifier{1, 3, 132, 0, 8}
)

type namedCurve struct {
	oid    asn1.ObjectIdentifier
	params *elliptic.CurveParams
}

// secp160r1 from SEC 2 v1 section 2.4.2. a = p - 3, which is what
// CurveParams assumes in its arithmetic.
var secp160r1 = namedCurve{
	oid: oidSecp160r1,
	params: &elliptic.CurveParams{
		Name:    "secp160r1",
		P:       hexInt("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF7FFFFFFF"),
		N:       hexInt("0100000000000000000001F4C8F927AED3CA752257"),
		B:       hexInt("1C97BEFC54BD7A8B65ACF89F81D4D4ADC565FA45"),
		Gx:      hexInt("4A96B5688EF573284664698968C38BB913CBFC82"),
		Gy:      hexInt("23A628553168947D59DCC912042351377AC5FB32"),
		BitSize: 160,
	},
}

var customCurves = []namedCurve{secp160r1}

func customCurveByOID(oid asn1.ObjectIdentifier) *namedCurve {
	for index := range customCurves {
		if customCurves[index].oid.Equal(oid) {
			return &customCurves[index]
		}
	}
	return nil
}

// customCurveOfAlgorithm returns the custom curve an ecPublicKey algorithm
// identifier names, or nil.
func customCurveOfAlgorithm(algorithm pkix.AlgorithmIdentifier) *namedCurve {
	if !algorithm.Algorithm.Equal(oidECPublicKey) {
		return nil
	}
	var oid asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(algorithm.Parameters.FullBytes, &oid); err != nil {
		return nil
	}
	return customCurveByOID(oid)
}

type pkcs8 struct {
	Version    int
	Algorithm  pkix.AlgorithmIdentifier
	PrivateKey []byte
}

// RFC 5915 ECPrivateKey.
type ecPrivateKey struct {
	Version       int
	PrivateKey    []byte
	NamedCurveOID asn1.ObjectIdentifier `asn1:"optional,explicit,tag:0"`
	PublicKey     asn1.BitString        `asn1:"optional,explicit,tag:1"`
}

type subjectPublicKeyInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	PublicKey asn1.BitString
}

func parsePKCS8(der []byte) (crypto.PrivateKey, error) {
	var wrapper pkcs8
	if rest, err := asn1.Unmarshal(der, &wrapper); err == nil && len(rest) == 0 {
		if curve := customCurveOfAlgorithm(wrapper.Algorithm); curve != nil {
			return parseCustomCurveKey(*curve, wrapper.PrivateKey)
		}
	}
	return x509.ParsePKCS8PrivateKey(der)
}

func parseSEC1(der []byte) (crypto.PrivateKey, error) {
	var inner ecPrivateKey
	if rest, err := asn1.Unmarshal(der, &inner); err == nil && len(rest) == 0 {
		if curve := customCurveByOID(inner.NamedCurveOID); curve != nil {
			return parseCustomCurveKey(*curve, der)
		}
	}
	return x509.ParseECPrivateKey(der)
}

func parseSPKI(der []byte) (crypto.PublicKey, error) {
	var info subjectPublicKeyInfo
	if rest, err := asn1.Unmarshal(der, &info); err == nil && len(rest) == 0 {
		if curve := customCurveOfAlgorithm(info.Algorithm); curve != nil {
			x, y := elliptic.Unmarshal(curve.params, info.PublicKey.RightAlign())
			if x == nil {
				return nil, fmt.Errorf("public key is not a valid %s point", curve.params.Name)
			}
			return &ecdsa.PublicKey{Curve: curve.params, X: x, Y: y}, nil
		}
	}
	return x509.ParsePKIXPublicKey(der)
}

// parseCustomCurveKey reads an RFC 5915 ECPrivateKey on curve and derives
// its public key from the scalar.
func parseCustomCurveKey(curve namedCurve, ecDER []byte) (*ecdsa.PrivateKey, error) {
	name := curve.params.Name
	var inner ecPrivateKey
	if rest, err := asn1.Unmarshal(ecDER, &inner); err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("private key is not a valid %s key", name)
	}
	if len(inner.NamedCurveOID) > 0 && !inner.NamedCurveOID.Equal(curve.oid) {
		return nil, errors.New("private key names two different curves")
	}
	d := new(big.Int).SetBytes(inner.PrivateKey)
	if d.Sign() <= 0 || d.Cmp(curve.params.N) >= 0 {
		return nil, fmt.Errorf("private key scalar is out of range for %s", name)
	}
	x, y := curve.params.ScalarBaseMult(inner.PrivateKey)
	if len(inner.PublicKey.Bytes) > 0 {
		px, py := elliptic.Unmarshal(curve.params, inner.PublicKey.RightAlign())
		if px == nil || px.Cmp(x) != 0 || py.Cmp(y) != 0 {
			return nil, errors.New("private key embeds a public key that does not match its scalar")
		}
	}
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve.params, X: x, Y: y},
		D:         d,
	}, nil
}

// marshalSPKI encodes a public key as SubjectPublicKeyInfo DER, byte for
// byte what OpenSSL produces, so it can be compared with a key embedded
// elsewhere.
func marshalSPKI(key crypto.PublicKey) ([]byte, error) {
	if public, ok := key.(*ecdsa.PublicKey); ok {
		for _, curve := range customCurves {
			if public.Curve != curve.params {
				continue
			}
			oid, err := asn1.Marshal(curve.oid)
			if err != nil {
				return nil, err
			}
			point := elliptic.Marshal(public.Curve, public.X, public.Y)
			return asn1.Marshal(subjectPublicKeyInfo{
				Algorithm: pkix.AlgorithmIdentifier{Algorithm: oidECPublicKey, Parameters: asn1.RawValue{FullBytes: oid}},
				PublicKey: asn1.BitString{Bytes: point, BitLength: 8 * len(point)},
			})
		}
	}
	return x509.MarshalPKIXPublicKey(key)
}

// orderBytes is the byte length of the curve's group order, which sizes
// each half of an IEEE P1363 signature (21 on secp160r1, whose order is 161
// bits, not 20).
func orderBytes(curve elliptic.Curve) int {
	return (curve.Params().N.BitLen() + 7) / 8
}

func hexInt(text string) *big.Int {
	value, ok := new(big.Int).SetString(text, 16)
	if !ok {
		panic("bad curve constant " + text)
	}
	return value
}
