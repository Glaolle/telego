package faketls

import (
	"encoding/binary"
	"testing"
)

func TestCipherSuiteGREASEVector(t *testing.T) {
	vector := func(suites ...uint16) []byte {
		payload := make([]byte, 39)
		payload = binary.BigEndian.AppendUint16(payload, uint16(2*len(suites)))
		for _, suite := range suites {
			payload = binary.BigEndian.AppendUint16(payload, suite)
		}
		return payload
	}
	for i := range 16 {
		grease := uint16(i*16+10) * 0x101
		if got := extractCipherSuite(vector(grease, 0xc02b, 0x1301), 0); got != 0xc02b {
			t.Fatalf("GREASE %04x selected %04x", grease, got)
		}
	}
	for _, suite := range []uint16{0x1a2a, 0x1301, 0xc02b} {
		if got := extractCipherSuite(vector(suite, 0x0a0a), 0); got != suite {
			t.Fatalf("ordinary suite %04x selected %04x", suite, got)
		}
	}
	for _, payload := range [][]byte{nil, vector(), vector(0x0a0a, 0xfafa), vector(0x1301)[:42]} {
		if got := extractCipherSuite(payload, 0); got != 0 {
			t.Fatalf("invalid/all-GREASE vector selected %04x", got)
		}
	}
	payload := vector(0x0a0a)
	payload = binary.BigEndian.AppendUint16(payload, 0x1301)
	if got := extractCipherSuite(payload, 0); got != 0 {
		t.Fatal("selected a suite beyond the declared vector")
	}
	payload = vector(0x1301)
	payload[40] = 1
	if got := extractCipherSuite(payload, 0); got != 0 {
		t.Fatal("accepted an odd cipher vector")
	}
}
