package gproxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/scratch-net/telego/pkg/transport/faketls"
	"github.com/scratch-net/telego/pkg/transport/obfuscated2"
)

func fragmentedHello(payload []byte, cuts ...int) []byte {
	var wire []byte
	start := 0
	for _, end := range append(cuts, len(payload)) {
		wire = append(wire, buildTLSRecord(faketls.RecordTypeHandshake, payload[start:end])...)
		start = end
	}
	return wire
}

func TestClientHelloPayloadFragments(t *testing.T) {
	key := []byte("0123456789abcdef")
	payload := buildValidClientHello(key, "example.com", bytes.Repeat([]byte{1}, 32))
	for cut := 1; cut < len(payload); cut++ {
		wire := fragmentedHello(payload, cut)
		for n := range len(wire) {
			_, _, ready, err := clientHelloPayload(wire[:n])
			if ready || err != nil {
				t.Fatalf("cut %d prefix %d: ready=%v err=%v", cut, n, ready, err)
			}
		}
		got, consumed, ready, err := clientHelloPayload(append(wire, 0x17, 3, 3, 0, 0))
		if err != nil || !ready || consumed != len(wire) || !bytes.Equal(got, payload) {
			t.Fatalf("cut %d failed: consumed=%d ready=%v err=%v", cut, consumed, ready, err)
		}
		if _, err := faketls.ParseClientHello(key, got); err != nil {
			t.Fatal(err)
		}
	}
	wire := fragmentedHello(payload, 1, 2, 3)
	if got, _, ready, err := clientHelloPayload(wire); err != nil || !ready || !bytes.Equal(got, payload) {
		t.Fatal("handshake header reassembly failed", err)
	}
	ordinary := buildTLSRecord(faketls.RecordTypeHandshake, payload)
	if allocs := testing.AllocsPerRun(100, func() { _, _, _, _ = clientHelloPayload(ordinary) }); allocs != 0 {
		t.Fatalf("ordinary record allocates: %v", allocs)
	}
}

func TestClientHelloPayloadInvalidFragments(t *testing.T) {
	payload := buildValidClientHello([]byte("0123456789abcdef"), "example.com", bytes.Repeat([]byte{1}, 32))
	valid := fragmentedHello(payload, 2)
	for _, index := range []int{7, 8, 9} {
		wire := bytes.Clone(valid)
		wire[index] = 0xff
		if _, _, _, err := clientHelloPayload(wire); !errors.Is(err, errClientHelloRecords) {
			t.Fatalf("invalid continuation at %d: %v", index, err)
		}
	}
	oversized := []byte{0x16, 3, 1, 0, 4, 1, 0xff, 0xff, 0xff}
	if _, _, _, err := clientHelloPayload(oversized); err == nil {
		t.Fatal("accepted oversized aggregate handshake")
	}
	var many []byte
	for _, b := range payload[:maxClientHelloRecords] {
		many = append(many, buildTLSRecord(faketls.RecordTypeHandshake, []byte{b})...)
	}
	if _, _, _, err := clientHelloPayload(many); err == nil {
		t.Fatal("accepted excessive fragmentation")
	}
	if _, _, _, err := clientHelloPayload([]byte{0x16, 3, 1, 0, 0}); err == nil {
		t.Fatal("accepted empty handshake record")
	}
	bad := bytes.Clone(payload)
	binary.BigEndian.PutUint16(bad[2:4], uint16(len(payload)-5))
	if _, _, _, err := clientHelloPayload(buildTLSRecord(0x16, bad)); err == nil {
		t.Fatal("accepted trailing bytes beyond declared handshake")
	}
}

func TestFragmentedFakeTLSHandlerPreservesO2(t *testing.T) {
	key := []byte("0123456789abcdef")
	handler := NewProxyHandler(&Config{Secrets: []Secret{{Name: "test", Key: key, Host: "example.com"}}, TimeSkewTolerance: time.Minute}, &testLogger{})
	captured := captureOutboundHandshake(handler)
	hello := fragmentedHello(buildValidClientHello(key, "example.com", bytes.Repeat([]byte{4}, 32)), 1, 3, 40)
	o2 := buildTLSRecord(faketls.RecordTypeApplicationData, buildDeterministicO2ClientFrame(t, key, -2, obfuscated2.ConnectionTypeIntermediate))
	conn := newTestMockGnetConn()
	ctx := NewConnContext()
	conn.SetContext(ctx)
	conn.SetReadData(append(hello, o2...))
	if action := handler.handleDetectProtocol(conn, ctx); action != gnet.None {
		t.Fatal(action)
	}
	assertOutboundFraming(t, ctx, waitForOutboundHandshake(t, captured), obfuscated2.ConnectionTypeIntermediate, -2)
}
