package testsupport

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestScriptedDiscordBoundsAndRedactsCapture(t *testing.T) {
	fake := NewScriptedDiscord(DiscordStep{Method: "POST", Path: "/x", Status: 204, BodyCheck: func(body []byte) error {
		if string(body) != "hello" {
			t.Fatalf("body mismatch")
		}
		return nil
	}})
	defer fake.Close()
	request, err := http.NewRequest("POST", "https://discord.com/x", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bot canary")
	client := &http.Client{Transport: fake.RoundTripper()}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	captured := fake.Requests()
	if len(captured) != 1 || captured[0].Headers["Authorization"] != "[REDACTED]" || captured[0].BodyLength != 5 {
		t.Fatalf("capture=%#v", captured)
	}
	if fake.Pending() != 0 || len(fake.Failures()) != 0 {
		t.Fatalf("pending=%d failures=%v", fake.Pending(), fake.Failures())
	}
}
