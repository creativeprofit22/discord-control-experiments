package cli

import (
	"strings"
	"testing"
)

func FuzzCLIParser(f *testing.F) {
	f.Add("inspect --config c --guild 1")
	f.Add("message create --input -")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}
		_, _ = Parse(strings.Fields(input))
	})
}
func FuzzStrictJSON(f *testing.F) {
	f.Add(`{"name":"ok"}`)
	f.Add(`{"name":"a","name":"b"}`)
	f.Fuzz(func(t *testing.T, input string) {
		if int64(len(input)) > DefaultInputLimit+1 {
			t.Skip()
		}
		var value struct {
			Name string `json:"name"`
		}
		_ = DecodeStrictJSON(strings.NewReader(input), DefaultInputLimit, &value)
	})
}
func FuzzContainedPath(f *testing.F) {
	f.Add("safe/file.json")
	f.Add("../escape")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		_, _ = ContainedPath(t.TempDir(), input, true)
	})
}
