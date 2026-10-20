//go:build linux

package gproxy

import (
	"github.com/panjf2000/gnet/v2"
	"golang.org/x/sys/unix"
)

const serverHelloPacingSupported = true

func serverHelloSocketBusy(conn gnet.Conn) (bool, error) {
	if conn.OutboundBuffered() != 0 {
		return true, nil
	}
	info, err := unix.GetsockoptTCPInfo(conn.Fd(), unix.IPPROTO_TCP, unix.TCP_INFO)
	if err != nil {
		return false, err
	}
	return info.Notsent_bytes != 0, nil
}
