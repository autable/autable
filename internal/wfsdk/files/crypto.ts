// autable workflow SDK. Vendored into workflow projects by `autablectl workflow sdk`; do not edit there.
//
// Hashes, MACs, signatures, ciphers, and byte encodings, computed by the
// server in Go. Every function is synchronous. Inputs typed BytesOrString
// take a string as its UTF-8 bytes; byte results are Uint8Arrays.

import { host } from "./host.js";
import type { Bytes, BytesOrString } from "./host.js";

export type HashAlgorithm = "sha1" | "sha256" | "sha512" | "md5";

/**
 * Signature algorithms. Keys are PEM text: private keys as PKCS #8
 * "PRIVATE KEY", "EC PRIVATE KEY" or "RSA PRIVATE KEY"; public keys as
 * "PUBLIC KEY" (PKIX), "RSA PUBLIC KEY", or a "CERTIFICATE" (verify only).
 * ECDSA signatures are produced as ASN.1 DER; verify also accepts the
 * 64-byte raw r‖s form WebCrypto uses.
 */
export type SignatureAlgorithm = "ed25519" | "ecdsa-p256-sha256" | "rsa-pkcs1-sha256";

/**
 * "aes-gcm": 12-byte IV recommended; the ciphertext is followed by the
 * 16-byte tag; optional additional authenticated data.
 * "aes-cbc": 16-byte IV, PKCS #7 padding, no aad.
 * A 16, 24, or 32-byte key selects AES-128, AES-192, or AES-256.
 */
export type CipherAlgorithm = "aes-gcm" | "aes-cbc";

const bytes = (buffer: ArrayBuffer) => new Uint8Array(buffer);

export const crypto = {
  /** Digest of data. `hex.encode(crypto.hash("sha256", "abc"))` is the usual hex form. */
  hash(algorithm: HashAlgorithm, data: BytesOrString): Uint8Array {
    return bytes(host().hash(algorithm, data));
  },
  /** HMAC of data under key, e.g. to sign or check a webhook payload. */
  hmac(algorithm: HashAlgorithm, key: BytesOrString, data: BytesOrString): Uint8Array {
    return bytes(host().hmac(algorithm, key, data));
  },
  /** n (0 to 65536) bytes from the operating system's secure random source. */
  randomBytes(n: number): Uint8Array {
    return bytes(host().randomBytes(n));
  },
  /** Signs data with a PEM private key. */
  sign(algorithm: SignatureAlgorithm, privateKeyPem: string, data: BytesOrString): Uint8Array {
    return bytes(host().sign(algorithm, privateKeyPem, data));
  },
  /** False for a wrong signature; throws for a malformed key. */
  verify(algorithm: SignatureAlgorithm, publicKeyPem: string, data: BytesOrString, signature: Bytes): boolean {
    return host().verify(algorithm, publicKeyPem, data, signature);
  },
  encrypt(algorithm: CipherAlgorithm, key: Bytes, iv: Bytes, plaintext: BytesOrString, aad?: BytesOrString): Uint8Array {
    return bytes(aad === undefined ? host().encrypt(algorithm, key, iv, plaintext) : host().encrypt(algorithm, key, iv, plaintext, aad));
  },
  /** Throws when authentication (aes-gcm) or the padding (aes-cbc) fails. */
  decrypt(algorithm: CipherAlgorithm, key: Bytes, iv: Bytes, ciphertext: Bytes, aad?: BytesOrString): Uint8Array {
    return bytes(aad === undefined ? host().decrypt(algorithm, key, iv, ciphertext) : host().decrypt(algorithm, key, iv, ciphertext, aad));
  },
  /** Compares two values in constant time (for equal lengths): use it to check MACs and tokens. */
  timingSafeEqual(a: BytesOrString, b: BytesOrString): boolean {
    return host().timingSafeEqual(a, b);
  },
};

function toBytes(data: BytesOrString): Bytes {
  return typeof data === "string" ? host().decode("utf8", data) : data;
}

export interface BinaryEncoding {
  /** Encodes bytes (a string is taken as its UTF-8 bytes). */
  encode(data: BytesOrString): string;
  /** Decodes text; throws on invalid input. */
  decode(text: string): Uint8Array;
}

/** Standard base64 with padding; decode also accepts unpadded input. */
export const base64: BinaryEncoding = {
  encode: (data) => host().encode("base64", toBytes(data)),
  decode: (text) => bytes(host().decode("base64", text)),
};

/** URL-safe base64 without padding; decode also accepts padded input. */
export const base64url: BinaryEncoding = {
  encode: (data) => host().encode("base64url", toBytes(data)),
  decode: (text) => bytes(host().decode("base64url", text)),
};

/** Lower-case hexadecimal. */
export const hex: BinaryEncoding = {
  encode: (data) => host().encode("hex", toBytes(data)),
  decode: (text) => bytes(host().decode("hex", text)),
};

/** UTF-8 text and its bytes. Invalid sequences decode to U+FFFD. */
export const utf8 = {
  /** The UTF-8 bytes of text. */
  encode(text: string): Uint8Array {
    return bytes(host().decode("utf8", text));
  },
  /** The text the bytes encode. */
  decode(data: Bytes): string {
    return host().encode("utf8", data);
  },
};
