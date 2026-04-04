package frame

import (
	"encoding/binary"
	"fmt"
	"io"
)

type Type byte

const MaxFramePayloadBytes uint32 = 16 * 1024 * 1024

const (
	TypeAttach Type = 0x01

	TypeDataClientToServer Type = 0x02
	TypeDataServerToClient Type = 0x03

	TypeResize      Type = 0x04
	TypeDetach      Type = 0x05
	TypeSnapshot    Type = 0x06
	TypeError       Type = 0x07
	TypeAgentExited Type = 0x08
)

type Frame struct {
	Type    Type
	Payload []byte
}

func WriteFrame(w io.Writer, frame Frame) error {
	if w == nil {
		return fmt.Errorf("write frame: writer is required")
	}
	if err := validateType(frame.Type); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	payload := frame.Payload
	if payload == nil {
		payload = []byte{}
	}
	if len(payload) > int(MaxFramePayloadBytes) {
		return fmt.Errorf("write frame: payload too large: %d", len(payload))
	}

	header := make([]byte, 5)
	header[0] = byte(frame.Type)
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))

	if err := writeAll(w, header); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}
	if err := writeAll(w, payload); err != nil {
		return fmt.Errorf("write frame payload: %w", err)
	}

	return nil
}

func ReadFrame(r io.Reader) (Frame, error) {
	if r == nil {
		return Frame{}, fmt.Errorf("read frame: reader is required")
	}

	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}

	typ := Type(header[0])
	if err := validateType(typ); err != nil {
		return Frame{}, fmt.Errorf("read frame: %w", err)
	}

	payloadLength := binary.LittleEndian.Uint32(header[1:])
	if payloadLength > MaxFramePayloadBytes {
		return Frame{}, fmt.Errorf("read frame: payload too large: %d", payloadLength)
	}
	payload := make([]byte, payloadLength)
	if payloadLength > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return Frame{}, err
		}
	}

	frame := Frame{Type: typ, Payload: payload}
	if err := validateType(frame.Type); err != nil {
		return Frame{}, fmt.Errorf("read frame result: %w", err)
	}

	return frame, nil
}

func IsValidType(typ Type) bool {
	switch typ {
	case TypeAttach,
		TypeDataClientToServer,
		TypeDataServerToClient,
		TypeResize,
		TypeDetach,
		TypeSnapshot,
		TypeError,
		TypeAgentExited:
		return true
	default:
		return false
	}
}

func validateType(typ Type) error {
	if !IsValidType(typ) {
		return fmt.Errorf("invalid frame type: 0x%02x", byte(typ))
	}

	return nil
}

func writeAll(w io.Writer, data []byte) error {
	remaining := data
	for len(remaining) > 0 {
		n, err := w.Write(remaining)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		remaining = remaining[n:]
	}

	return nil
}
