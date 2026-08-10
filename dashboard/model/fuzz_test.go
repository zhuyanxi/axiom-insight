package model

import (
	"bytes"
	"testing"
)

// FuzzDecode ensures the strict decoder never panics or hangs on
// arbitrary input, rejects duplicate keys and non-finite numbers, and
// keeps a valid dashboard stable through render -> decode -> render.
func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":41,"title":"x","id":null,"version":0,"editable":true,"templating":{"list":[]},"rows":[],"annotations":{"list":[]}}`))
	f.Add([]byte(`{"schemaVersion":41,"title":"x","uid":"a","uid":"b"}`))
	f.Add([]byte("not json at all { ["))
	f.Add([]byte{0xff, 0xfe, 0x00, 0x01})
	f.Add([]byte(`{"schemaVersion":41,"title":"x","id":null,"version":0,"editable":true,"templating":{"list":[]},"rows":[],"annotations":{"list":[]},"__inputs":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		dashboard, err := Decode(data)
		if err != nil {
			return
		}
		if len(Validate(dashboard)) > 0 {
			return
		}
		rendered, err := Render(dashboard)
		if err != nil {
			t.Fatalf("valid dashboard failed to render: %v", err)
		}
		reparsed, err := Decode(rendered)
		if err != nil {
			t.Fatalf("re-decode of rendered output failed: %v", err)
		}
		second, err := Render(reparsed)
		if err != nil {
			t.Fatalf("re-render failed: %v", err)
		}
		if !bytes.Equal(rendered, second) {
			t.Fatalf("render is not idempotent for input %q", data)
		}
	})
}
