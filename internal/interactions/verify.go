package interactions

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

var ErrSignature = errors.New("interaction signature rejected")

type VerificationOutcome string

const (
	VerificationValid     VerificationOutcome = "valid"
	VerificationMissing   VerificationOutcome = "missing"
	VerificationMalformed VerificationOutcome = "malformed"
	VerificationInvalid   VerificationOutcome = "invalid"
	VerificationStale     VerificationOutcome = "stale"
)

type Verifier struct {
	keys []ed25519.PublicKey
	skew time.Duration
	now  func() time.Time
}

func NewVerifier(encodedKeys []string, skew time.Duration, now func() time.Time) (*Verifier, error) {
	if len(encodedKeys) < 1 || len(encodedKeys) > 2 || skew <= 0 || now == nil {
		return nil, errors.New("verifier dependencies are required")
	}
	keys := make([]ed25519.PublicKey, 0, len(encodedKeys))
	seen := map[string]bool{}
	for _, encoded := range encodedKeys {
		decoded, err := hex.DecodeString(encoded)
		if err != nil || len(decoded) != ed25519.PublicKeySize || len(encoded) != 64 || seen[encoded] {
			return nil, errors.New("invalid signing public key")
		}
		seen[encoded] = true
		keys = append(keys, ed25519.PublicKey(decoded))
	}
	return &Verifier{keys: keys, skew: skew, now: now}, nil
}

func (v *Verifier) Verify(timestampHeader, signatureHeader string, body []byte) (int, VerificationOutcome, error) {
	if timestampHeader == "" || signatureHeader == "" {
		return -1, VerificationMissing, ErrSignature
	}
	if len(timestampHeader) > 20 || len(signatureHeader) != ed25519.SignatureSize*2 {
		return -1, VerificationMalformed, ErrSignature
	}
	seconds, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil || seconds < 1 || strconv.FormatInt(seconds, 10) != timestampHeader {
		return -1, VerificationMalformed, ErrSignature
	}
	signature, err := hex.DecodeString(signatureHeader)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return -1, VerificationMalformed, ErrSignature
	}
	delta := v.now().Sub(time.Unix(seconds, 0))
	if delta < -v.skew || delta > v.skew {
		return -1, VerificationStale, ErrSignature
	}
	message := make([]byte, 0, len(timestampHeader)+len(body))
	message = append(message, timestampHeader...)
	message = append(message, body...)
	matched := -1
	for index, key := range v.keys {
		if ed25519.Verify(key, message, signature) {
			matched = index
		}
	}
	if matched < 0 {
		return -1, VerificationInvalid, ErrSignature
	}
	return matched, VerificationValid, nil
}
