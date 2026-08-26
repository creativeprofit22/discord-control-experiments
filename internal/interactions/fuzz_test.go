package interactions

import "testing"

func FuzzDecodeVerifiedInteraction(f *testing.F) {
	f.Add([]byte(`{"id":"1","application_id":"2","type":1,"token":"x"}`), "2")
	f.Add([]byte(`{"type":2,"data":{"options":[{"options":[{}]}]}}`), "2")
	f.Fuzz(func(t *testing.T, body []byte, applicationID string) {
		if len(body) > 128<<10 || len(applicationID) > 128 {
			return
		}
		_, _ = DecodeVerified(body, applicationID)
	})
}
