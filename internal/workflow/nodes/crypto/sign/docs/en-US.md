# Sign data

Signs a string with a private key and returns the signature. The key lives in
the node's secrets, so a script never sees it: the script hands over the text,
the node hands back the signature. Use it wherever something has to be
issued that a client can check offline against an embedded public key — a
license, an activation code, a signed token.

## Secret

- `private_key` — a PKCS#8 private key, either base64 DER on one line or
  PEM. Only ECDSA keys are accepted; the curve comes from the key itself and
  may be P-256, P-384, P-521 or secp160r1. The short curve exists for codes
  that get typed by hand: it signs through Go's generic, non-constant-time
  ECDSA path, which is fine for a server issuing licences and not for keys an
  attacker can time.

## Inputs

- `data` — the text to sign. Its UTF-8 bytes are what gets signed, exactly as
  given: trim and normalise in the script before calling.
- `algorithm`, `hash`, `format` — optional. Each has exactly one supported
  value, which is also the default: `ecdsa`, `sha256`, `p1363`. Asking for
  anything else is an error rather than a fallback, so a script written for a
  variant that does not exist yet fails at the call.
- `encoding` — `base64` (default) or `base58`. Base58 uses the Bitcoin
  alphabet (no `0`, `O`, `I`, `l`) and is fixed-width: a signature of a given
  byte length always yields the same number of digits (42 bytes give 58),
  padded with leading `1`s, so a verifier can check the length outright.

`p1363` is the raw `r||s` layout, each half zero-padded to the size of the
group order (32 bytes each on P-256, 21 each on secp160r1 whose order is 161
bits). It is what .NET's `ECDsa.VerifyData` and Windows CNG expect natively;
use it unless the verifier specifically wants DER.

## Outputs

- `signature` — the encoded signature.
- `public_key` — the matching public key as base64 SPKI, encoded exactly as
  OpenSSL does. Compare it with the one embedded in the client to confirm the
  right key is configured.
- `curve`, `algorithm`, `hash`, `format`, `encoding` — what was used.

The node verifies its own output against the derived public key before
returning it, so a signature you receive is one the matching verifier will
accept.
