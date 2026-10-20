//go:build !linux

package gproxy

import "github.com/panjf2000/gnet/v2"

const serverHelloPacingSupported = false

func serverHelloSocketBusy(gnet.Conn) (bool, error) { return false, nil }
