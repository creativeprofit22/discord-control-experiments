package interactions

import (
	"fmt"
	"testing"
)

func TestDecodeSupportedInteractionTypesAndIdentity(t *testing.T) {
	applicationID := "123456789012345678"
	common := `"id":"111","application_id":"%s","token":"token","guild_id":"222","member":{"user":{"id":"333"}}`
	cases := []string{
		`{"id":"111","application_id":"123456789012345678","token":"token","type":1}`,
		`{` + fmt.Sprintf(common, applicationID) + `,"type":2,"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"health","type":1}]}}`,
		`{` + fmt.Sprintf(common, applicationID) + `,"type":3,"data":{"custom_id":"cancel.ABCDEFGHIJKLMNOPQRSTUVWX","component_type":2}}`,
		`{` + fmt.Sprintf(common, applicationID) + `,"type":4,"data":{"id":"444","name":"discord-control","type":1,"options":[{"name":"health","type":1,"focused":true}]}}`,
		`{` + fmt.Sprintf(common, applicationID) + `,"type":5,"data":{"custom_id":"resume_modal.ABCDEFGHIJKLMNOPQRSTUVWX","components":[{"type":1,"components":[{"type":4,"custom_id":"confirmation","value":"aaaaaaaaaaaa"}]}]}}`,
	}
	for index, body := range cases {
		if _, err := DecodeVerified([]byte(body), applicationID); err != nil {
			t.Fatalf("type case %d: %v", index+1, err)
		}
	}
}
func TestDecodeRejectsWrongApplicationGuildAndUnknownJSON(t *testing.T) {
	cases := []string{
		`{"id":"111","application_id":"999","token":"token","type":1}`,
		`{"id":"111","application_id":"123456789012345678","token":"token","type":2,"guild_id":"222","member":{"user":{"id":"333"},"guild_id":"444"},"data":{"id":"1","name":"discord-control","type":1}}`,
		`{"id":"111","application_id":"123456789012345678","token":"token","type":1,"unexpected":true}`,
		`{"id":"111","application_id":"123456789012345678","token":"token","type":1} {}`,
	}
	for index, body := range cases {
		if _, err := DecodeVerified([]byte(body), "123456789012345678"); err == nil {
			t.Fatalf("invalid case %d accepted", index)
		}
	}
}

func TestMarshalDeferredCallbackJSON(t *testing.T) {
	messageUpdate, err := marshalDeferredCallback(CallbackDeferredMessageUpdate)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(messageUpdate), `{"type":6}`; got != want {
		t.Fatalf("deferred message update JSON = %s, want %s", got, want)
	}

	channelMessage, err := marshalDeferredCallback(CallbackDeferredChannelMessage)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(channelMessage), `{"type":5,"data":{"flags":64}}`; got != want {
		t.Fatalf("deferred channel message JSON = %s, want %s", got, want)
	}
}
