package sign

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// Go's x509 only understands the NIST curves, so PKCS#8 is parsed here and
// dispatched by the curve OID: NIST curves go through x509, and the curves
// listed in customCurves are built from their parameters. Signing on a custom
// curve uses crypto/ecdsa's generic big.Int implementation, which is not
// constant-time; acceptable for issuing licences from a server, not for
// anything an attacker can time at will.

var (
	oidECPublicKey = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidSecp160r1   = asn1.ObjectIdentifier{1, 3, 132, 0, 8}
)

type namedCurve struct {
	name   string
	oid    asn1.ObjectIdentifier
	params *elliptic.CurveParams
}

// secp160r1 from SEC 2 v2 section 2.4.2. a = p - 3, which is what CurveParams
// assumes in its arithmetic.
var secp160r1 = namedCurve{
	name: "secp160r1",
	oid:  oidSecp160r1,
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

// parsePKCS8ECDSA returns the private key and the curve name it lives on.
func parsePKCS8ECDSA(der []byte) (*ecdsa.PrivateKey, string, error) {
	var wrapper pkcs8
	if rest, err := asn1.Unmarshal(der, &wrapper); err != nil || len(rest) != 0 {
		return nil, "", errors.New("private_key is not a PKCS#8 key")
	}
	if !wrapper.Algorithm.Algorithm.Equal(oidECPublicKey) {
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, "", fmt.Errorf("private_key is not a PKCS#8 key: %w", err)
		}
		return nil, "", fmt.Errorf("private_key is a %T, only ECDSA keys are supported", parsed)
	}
	var curveOID asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(wrapper.Algorithm.Parameters.FullBytes, &curveOID); err != nil {
		return nil, "", errors.New("private_key does not name its curve")
	}
	for _, curve := range customCurves {
		if curve.oid.Equal(curveOID) {
			key, err := parseCustomCurveKey(curve, wrapper.PrivateKey)
			return key, curve.name, err
		}
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, "", fmt.Errorf("private_key is not a PKCS#8 key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, "", fmt.Errorf("private_key is a %T, only ECDSA keys are supported", parsed)
	}
	return key, key.Curve.Params().Name, nil
}

func parseCustomCurveKey(curve namedCurve, ecDER []byte) (*ecdsa.PrivateKey, error) {
	var inner ecPrivateKey
	if rest, err := asn1.Unmarshal(ecDER, &inner); err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("private_key is not a valid %s key", curve.name)
	}
	if len(inner.NamedCurveOID) > 0 && !inner.NamedCurveOID.Equal(curve.oid) {
		return nil, fmt.Errorf("private_key names two different curves")
	}
	d := new(big.Int).SetBytes(inner.PrivateKey)
	if d.Sign() <= 0 || d.Cmp(curve.params.N) >= 0 {
		return nil, fmt.Errorf("private_key scalar is out of range for %s", curve.name)
	}
	x, y := curve.params.ScalarBaseMult(inner.PrivateKey)
	if len(inner.PublicKey.Bytes) > 0 {
		px, py := elliptic.Unmarshal(curve.params, inner.PublicKey.Bytes)
		if px == nil || px.Cmp(x) != 0 || py.Cmp(y) != 0 {
			return nil, errors.New("private_key embeds a public key that does not match its scalar")
		}
	}
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve.params, X: x, Y: y},
		D:         d,
	}, nil
}

// marshalSPKI encodes the public key as SubjectPublicKeyInfo, byte-for-byte
// what OpenSSL produces, so it can be compared with a key embedded elsewhere.
func marshalSPKI(key *ecdsa.PublicKey) ([]byte, error) {
	for _, curve := range customCurves {
		if key.Curve == curve.params {
			oid, err := asn1.Marshal(curve.oid)
			if err != nil {
				return nil, err
			}
			return asn1.Marshal(subjectPublicKeyInfo{
				Algorithm: pkix.AlgorithmIdentifier{Algorithm: oidECPublicKey, Parameters: asn1.RawValue{FullBytes: oid}},
				PublicKey: asn1.BitString{Bytes: elliptic.Marshal(key.Curve, key.X, key.Y), BitLength: 8 * (2*((key.Curve.Params().BitSize+7)/8) + 1)},
			})
		}
	}
	return x509.MarshalPKIXPublicKey(key)
}

func hexInt(text string) *big.Int {
	value, ok := new(big.Int).SetString(text, 16)
	if !ok {
		panic("bad curve constant " + text)
	}
	return value
}
