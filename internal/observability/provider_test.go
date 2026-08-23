package observability

import "testing"

func TestOTLPSignalEndpointPreservesPre145Paths(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		defaultPath string
		want        string
	}{
		{"trace default path", "https://collector.example", "/v1/traces", "https://collector.example/v1/traces"},
		{"metric default path", "https://collector.example", "/v1/metrics", "https://collector.example/v1/metrics"},
		{"explicit path", "https://collector.example/custom", "/v1/traces", "https://collector.example/custom"},
		{"explicit root path", "https://collector.example/", "/v1/metrics", "https://collector.example/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := otlpSignalEndpoint(test.endpoint, test.defaultPath)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("endpoint = %q, want %q", got, test.want)
			}
		})
	}
}

func TestOTLPSignalEndpointRejectsMalformedURL(t *testing.T) {
	if _, err := otlpSignalEndpoint("https://%", "/v1/traces"); err == nil {
		t.Fatal("expected malformed endpoint rejection")
	}
}
