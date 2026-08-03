package generator

import (
	"bytes"
	"testing"
)

// FuzzStrictDecode ensures the strict decoder never panics or hangs on
// arbitrary input, and that a valid document stays stable through
// render -> decode -> render.
func FuzzStrictDecode(f *testing.F) {
	f.Add([]byte(readFixtureForSeed("valid/metrics.yaml")))
	f.Add([]byte("schema_version: generator.metrics/v1"))
	f.Add([]byte("not yaml at all { ["))
	f.Add([]byte{0xff, 0xfe, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, decode := range []func([]byte) error{
			func(input []byte) error { _, err := DecodeMetrics(input); return err },
			func(input []byte) error { _, err := DecodeOTel(input); return err },
			func(input []byte) error { _, err := DecodeLogging(input); return err },
		} {
			_ = decode(data)
		}

		document, err := DecodeMetrics(data)
		if err != nil {
			return
		}
		if len(document.Validate()) > 0 {
			return
		}
		rendered, err := RenderMetrics(document)
		if err != nil {
			t.Fatalf("valid document failed to render: %v", err)
		}
		reparsed, err := DecodeMetrics(rendered)
		if err != nil {
			t.Fatalf("re-decode of rendered output failed: %v", err)
		}
		second, err := RenderMetrics(reparsed)
		if err != nil {
			t.Fatalf("re-render failed: %v", err)
		}
		if !bytes.Equal(rendered, second) {
			t.Fatalf("render is not idempotent for input %q", data)
		}
	})
}

// readFixtureForSeed reads a fixture without requiring a *testing.T, for use
// as fuzz seed data.
func readFixtureForSeed(relative string) []byte {
	data, err := readFile(fixturePath(relative))
	if err != nil {
		panic(err)
	}
	return data
}
