package iossimdriver

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// wsOpcodeText is the RFC 6455 opcode for a text frame. The helper
// speaks newline-terminated JSON exclusively over text frames.
const wsOpcodeText = 0x1

// maxWsFramePayload bounds a single outgoing frame. A HID gesture
// message never approaches this size; it exists as a guard rather
// than a real limit ever hit in practice.
const maxWsFramePayload = 65535

// encodeWsTextFrame wraps payload in a single, masked, FIN-set text
// frame. RFC 6455 §5.1 requires every client-to-server frame to be
// masked; the helper, as the server side, never masks its replies.
func encodeWsTextFrame(payload []byte) ([]byte, error) {
	n := len(payload)
	if n > maxWsFramePayload {
		return nil, &HidClientError{Message: "hid-client: payload too large", Code: HidClientErrNotConnected}
	}

	var header []byte
	switch {
	case n <= 125:
		header = make([]byte, 6)
		header[0] = 0x80 | wsOpcodeText
		header[1] = 0x80 | byte(n)
	default:
		header = make([]byte, 8)
		header[0] = 0x80 | wsOpcodeText
		header[1] = 0x80 | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(n))
	}
	maskOffset := len(header) - 4
	if _, err := rand.Read(header[maskOffset:]); err != nil {
		return nil, fmt.Errorf("iossimdriver: generate frame mask: %w", err)
	}
	mask := header[maskOffset:]

	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}
	return append(header, masked...), nil
}

// extractWsFrame attempts to parse and consume one complete WS frame
// from the front of *buf. It reports ok=false when *buf does not yet
// hold a complete frame, leaving *buf untouched. On success it
// returns the frame's opcode and unmasked payload, having advanced
// *buf past the consumed bytes.
//
// The helper never sends masked frames (server-to-client frames are
// unmasked per RFC 6455 §5.1), but this also unmasks when the mask
// bit is set so the codec tolerates either direction, matching what a
// test harness playing the server role may choose to send.
func extractWsFrame(buf *[]byte) (opcode byte, payload []byte, ok bool) {
	b := *buf
	if len(b) < 2 {
		return 0, nil, false
	}
	opcode = b[0] & 0x0f
	isMasked := b[1]&0x80 != 0
	length := int(b[1] & 0x7f)
	offset := 2

	switch length {
	case 126:
		if len(b) < 4 {
			return 0, nil, false
		}
		length = int(binary.BigEndian.Uint16(b[2:4]))
		offset = 4
	case 127:
		if len(b) < 10 {
			return 0, nil, false
		}
		length = int(binary.BigEndian.Uint64(b[2:10]))
		offset = 10
	}

	maskLen := 0
	if isMasked {
		maskLen = 4
	}
	total := offset + maskLen + length
	if len(b) < total {
		return 0, nil, false
	}

	var mask []byte
	if isMasked {
		mask = b[offset : offset+4]
		offset += 4
	}
	payload = append([]byte(nil), b[offset:offset+length]...)
	if mask != nil {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}

	*buf = append([]byte(nil), b[total:]...)
	return opcode, payload, true
}
