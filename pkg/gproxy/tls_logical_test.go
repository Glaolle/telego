package gproxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestLogicalFragmentedHelloBudgetAndReplay(t *testing.T) {
	key := []byte("0123456789abcdef")
	handler := NewProxyHandler(&Config{
		Secrets:           []Secret{{Name: "test", Key: key, Host: "example.com"}},
		TimeSkewTolerance: time.Minute, ServerHelloSegmentSize: 92,
	}, &testLogger{})
	x := newLogicalTestStream(t, handler, nil)
	x.output.blocked.Store(true)
	hello := fragmentedHello(buildValidClientHello(key, "example.com", bytes.Repeat([]byte{0x71}, 32)), 1, 3, 40)
	x.write(t, hello)
	time.Sleep(20 * time.Millisecond)
	if x.stream.ctx.State() != StateReadTLSPayload || x.stream.ReadableBytes() != 0 {
		t.Fatal("blocked fragmented hello changed authentication state")
	}
	x.output.blocked.Store(false)
	deadline := time.Now().Add(time.Second)
	for x.stream.ctx.State() != StateReadO2Frame {
		if time.Now().After(deadline) {
			t.Fatal("fragmented hello did not resume on logical carrier")
		}
		time.Sleep(time.Millisecond)
	}
	if x.stream.ctx.helloPacer.Load() != nil {
		t.Fatal("logical carrier used TCP pacing")
	}
	y := newLogicalTestStream(t, handler, nil)
	y.write(t, hello)
	y.awaitClosed(t)
}

func TestLogicalFragmentedHelloPreservesSpliceBytes(t *testing.T) {
	key := []byte("0123456789abcdef")
	handler := NewProxyHandler(&Config{Secrets: []Secret{{Name: "test", Key: key, Host: "example.com"}}, SpliceHost: "127.0.0.1", SplicePort: 443}, &testLogger{})
	peer, socket := directTCPPair(t)
	startLogicalDC(t, handler, socket)
	handler.spliceDial = func(context.Context, string) (net.Conn, error) { return socket, nil }
	x := newLogicalTestStream(t, handler, nil)
	hello := fragmentedHello(buildValidClientHello([]byte("different secret"), "example.com", bytes.Repeat([]byte{0x72}, 32)), 1, 2, 3, 41)
	hello = append(hello, []byte("coalesced unrecognized tail")...)
	x.write(t, hello)
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(hello))
	if _, err := io.ReadFull(peer, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, hello) {
		t.Fatal("splice changed original fragmented wire bytes")
	}
	_ = peer.Close()
	x.awaitClosed(t)
}
