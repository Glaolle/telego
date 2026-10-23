package gproxy

import (
	"encoding/binary"
	"errors"

	"github.com/scratch-net/telego/pkg/transport/faketls"
)

const maxClientHelloRecords = 32

var errClientHelloRecords = errors.New("invalid or oversized ClientHello records")

// clientHelloPayload retains the original wire input until authentication. A
// complete ordinary record aliases data; only fragmented hellos allocate.
func clientHelloPayload(data []byte) (payload []byte, consumed int, ready bool, err error) {
	// Keep the bounded fragment bookkeeping off the ordinary record stack.
	if len(data) >= faketls.RecordHeaderSize {
		size := int(binary.BigEndian.Uint16(data[3:5]))
		version := binary.BigEndian.Uint16(data[1:3])
		if size >= 39 && size <= faketls.MaxRecordPayload && len(data) >= faketls.RecordHeaderSize+size &&
			data[0] == faketls.RecordTypeHandshake && version >= faketls.VersionTLS10 && version <= faketls.VersionTLS12 {
			payload := data[faketls.RecordHeaderSize : faketls.RecordHeaderSize+size]
			declared := 4 + (int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3]))
			if payload[0] == 1 && declared == size {
				return payload, faketls.RecordHeaderSize + size, true, nil
			}
		}
	}
	return fragmentedClientHelloPayload(data)
}

func fragmentedClientHelloPayload(data []byte) (payload []byte, consumed int, ready bool, err error) {
	var spans [maxClientHelloRecords][2]int
	var header [4]byte
	headerBytes, total, expected := 0, 0, 0
	for record := range maxClientHelloRecords {
		if len(data)-consumed < faketls.RecordHeaderSize {
			return nil, 0, false, nil
		}
		h := data[consumed : consumed+faketls.RecordHeaderSize]
		version := binary.BigEndian.Uint16(h[1:3])
		size := int(binary.BigEndian.Uint16(h[3:5]))
		if h[0] != faketls.RecordTypeHandshake || version < faketls.VersionTLS10 || version > faketls.VersionTLS12 ||
			size == 0 || size > faketls.MaxRecordPayload-total {
			return nil, 0, false, errClientHelloRecords
		}
		start := consumed + faketls.RecordHeaderSize
		end := start + size
		if end > len(data) {
			return nil, 0, false, nil
		}
		spans[record] = [2]int{start, end}
		if headerBytes < len(header) {
			headerBytes += copy(header[headerBytes:], data[start:end])
			if header[0] != 1 {
				return nil, 0, false, errClientHelloRecords
			}
			if headerBytes == len(header) {
				expected = 4 + (int(header[1])<<16 | int(header[2])<<8 | int(header[3]))
				if expected < 39 || expected > faketls.MaxRecordPayload {
					return nil, 0, false, errClientHelloRecords
				}
			}
		}
		total += size
		consumed = end
		if expected == 0 || total < expected {
			continue
		}
		if total != expected {
			return nil, 0, false, errClientHelloRecords
		}
		if record == 0 {
			return data[start:end], consumed, true, nil
		}
		payload = make([]byte, 0, total)
		for _, span := range spans[:record+1] {
			payload = append(payload, data[span[0]:span[1]]...)
		}
		return payload, consumed, true, nil
	}
	return nil, 0, false, errClientHelloRecords
}
