package interactions

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"
)

func testSigningKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	private := ed25519.NewKeyFromSeed(seed)
	return private.Public().(ed25519.PublicKey), private
}
func signInteraction(private ed25519.PrivateKey, timestamp string, body []byte) string {
	message := append(append([]byte(nil), timestamp...), body...)
	return hex.EncodeToString(ed25519.Sign(private, message))
}

func TestVerifierExactTimestampAndRawBody(t *testing.T) {
	public, private := testSigningKey(t)
	now := time.Unix(1_800_000_000, 0)
	timestamp := "1800000000"
	body := []byte("{\n  \"type\": 1\n}")
	verifier, err := NewVerifier([]string{hex.EncodeToString(public)}, 5*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if slot, outcome, err := verifier.Verify(timestamp, signInteraction(private, timestamp, body), body); err != nil || slot != 0 || outcome != VerificationValid {
		t.Fatalf("valid verification = %d %s %v", slot, outcome, err)
	}
	mutations := [][]byte{[]byte("{\"type\":1}"), append(append([]byte(nil), body...), ' '), bytesReplace(body, '1', '2')}
	for _, mutated := range mutations {
		if _, outcome, err := verifier.Verify(timestamp, signInteraction(private, timestamp, body), mutated); err == nil || outcome != VerificationInvalid {
			t.Fatalf("mutation accepted: %q", mutated)
		}
	}
	if _, outcome, err := verifier.Verify("1799999000", signInteraction(private, "1799999000", body), body); err == nil || outcome != VerificationStale {
		t.Fatalf("stale outcome = %s", outcome)
	}
}
func TestVerifierDualKeyRotationAndMalformedHeaders(t *testing.T) {
	public1, _ := testSigningKey(t)
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 99
	private2 := ed25519.NewKeyFromSeed(seed)
	public2 := private2.Public().(ed25519.PublicKey)
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{}`)
	verifier, err := NewVerifier([]string{hex.EncodeToString(public1), hex.EncodeToString(public2)}, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if slot, _, err := verifier.Verify("1800000000", signInteraction(private2, "1800000000", body), body); err != nil || slot != 1 {
		t.Fatalf("rotation slot = %d, %v", slot, err)
	}
	for _, headers := range [][2]string{{"", ""}, {"x", "00"}, {"1800000000", "zz"}} {
		if _, _, err := verifier.Verify(headers[0], headers[1], body); err == nil {
			t.Fatalf("malformed headers accepted: %#v", headers)
		}
	}
}
func bytesReplace(value []byte, from, to byte) []byte {
	result := append([]byte(nil), value...)
	for i := range result {
		if result[i] == from {
			result[i] = to
			break
		}
	}
	return result
}
