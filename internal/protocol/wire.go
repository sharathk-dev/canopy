package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame type bytes for the binary IPC protocol.
const (
	FrameJSON   byte = 0x01 // JSON-encoded Cmd or Response
	FramePTY    byte = 0x02 // raw terminal bytes
	FrameResize byte = 0x03 // resize: JSON {"Rows":N,"Cols":N}
)

// WriteFrame writes a length-prefixed frame to w.
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	header := [5]byte{typ}
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one frame from r, returning its type and payload.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	typ := header[0]
	length := binary.LittleEndian.Uint32(header[1:])
	if length > 64*1024*1024 { // 64 MiB sanity cap
		return 0, nil, fmt.Errorf("frame too large: %d bytes", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return typ, payload, nil
}
