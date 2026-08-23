package interactions

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type pongResponder struct{ calls int }

func (r *pongResponder) Respond(_ context.Context, interaction VerifiedInteraction) ([]byte, error) {
	r.calls++
	return marshalCallback(Callback{Type: CallbackPong})
}
func testServer(t *testing.T, now time.Time) (*Server, *pongResponder, ed25519.PrivateKey) {
	t.Helper()
	public, private := testSigningKey(t)
	verifier, err := NewVerifier([]string{hex.EncodeToString(public)}, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	responder := &pongResponder{}
	server, err := NewServer(ServerOptions{ListenAddress: "127.0.0.1:0", Path: "/interactions", ApplicationID: "123456789012345678", Verifier: verifier, Responder: responder, MaxBodyBytes: 4096, MaxConcurrentRequests: 2, AckDeadline: 2500 * time.Millisecond, TLSMode: "direct", TLSCertificate: &tls.Certificate{}, CorrelationID: func() (domain.CorrelationID, error) { return "correlation", nil }})
	if err != nil {
		t.Fatal(err)
	}
	return server, responder, private
}

func TestServerVerifiesBeforeDecodeAndPongs(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	server, responder, private := testServer(t, now)
	timestamp := "1800000000"
	body := []byte(`{"id":"111","application_id":"123456789012345678","type":1,"token":"token"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "https://example.test/interactions", strings.NewReader(string(body)))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Signature-Timestamp", timestamp)
	request.Header.Set("X-Signature-Ed25519", signInteraction(private, timestamp, body))
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"type":1}` || responder.calls != 1 {
		t.Fatalf("valid response = %d %q calls=%d", recorder.Code, recorder.Body.String(), responder.calls)
	}
	malformed := []byte(`{"id":`)
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "https://example.test/interactions", strings.NewReader(string(malformed)))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Signature-Timestamp", timestamp)
	request.Header.Set("X-Signature-Ed25519", strings.Repeat("0", 128))
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || responder.calls != 1 {
		t.Fatalf("invalid signature reached decoder/responder: %d calls=%d", recorder.Code, responder.calls)
	}
	request = httptest.NewRequest(http.MethodPost, "https://example.test/interactions", strings.NewReader(string(malformed)))
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Signature-Timestamp", timestamp)
	request.Header.Set("X-Signature-Ed25519", signInteraction(private, timestamp, malformed))
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("signed malformed JSON = %d", recorder.Code)
	}
}
func TestServerHealthAndExactRoutes(t *testing.T) {
	server, _, _ := testServer(t, time.Now())
	for path, want := range map[string]int{"/livez": 200, "/readyz": 503, "/metrics": 404, "/interactions/": 404} {
		request := httptest.NewRequest(http.MethodGet, "https://example.test"+path, nil)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Fatalf("%s = %d, want %d", path, recorder.Code, want)
		}
		if strings.Contains(recorder.Body.String(), "123") {
			t.Fatal("health leaked configuration")
		}
	}
}
