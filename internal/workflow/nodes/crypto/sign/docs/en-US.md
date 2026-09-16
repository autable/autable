# Sign data

Signs a string with a private key and returns the signature. The key lives in
the node's secrets, so a script never sees it: the script hands over the text,
the node hands back the signature. Use it wherever something has to be
issued that a client can check offline against an embedded public key — a
license, an activation code, a signed token.

## Secret

- `private_key` — a PKCS#8 private key, either base64 DER on one line or
  PEM. Only ECDSA keys are accepted; the curve comes from the key itself.

## Inputs

- `data` — the text to sign. Its UTF-8 bytes are what gets signed, exactly as
  given: trim and normalise in the script before calling.
- `algorithm`, `hash`, `format`, `encoding` — optional. Each has exactly one
  supported value, which is also the default: `ecdsa`, `sha256`, `p1363`,
  `base64`. Asking for anything else is an error rather than a fallback, so a
  script written for a variant that does not exist yet fails at the call.

`p1363` is the raw `r||s` layout, each half zero-padded to the curve size (64
bytes on P-256). It is what .NET's `ECDsa.VerifyData` and Windows CNG expect
natively; use it unless the verifier specifically wants DER.

## Outputs

- `signature` — the encoded signature.
- `public_key` — the matching public key as base64 SPKI. Compare it with the
  one embedded in the client to confirm the right key is configured.
- `algorithm`, `hash`, `format`, `encoding` — what was used.

The node verifies its own output against the derived public key before
returning it, so a signature you receive is one the matching verifier will
accept.
