package protocol

import (
	"encoding/binary"
	"errors"
)

type PackageType uint32

const (
	Heartbeat      PackageType = 20
	Awake          PackageType = 21
	Clipboard      PackageType = 69
	ClipboardAsk   PackageType = 78
	ClipboardPush  PackageType = 79
	NextMachine    PackageType = 121
	Keyboard       PackageType = 122
	Mouse          PackageType = 123
	ClipboardText  PackageType = 124
	ClipboardImage PackageType = 125
	Handshake      PackageType = 126
	HandshakeAck   PackageType = 127
	Matrix         PackageType = 128
)

const (
	PackageSize   = 32
	PackageSizeEx = 64
)

// Header represents the 24-byte common header
type Header struct {
	Type     PackageType
	Id       uint32
	Src      uint32
	Des      uint32
	DateTime uint64
}

// KeyboardData 8 bytes
type KeyboardData struct {
	Vk    int32
	Flags int32
}

// MouseData 16 bytes
type MouseData struct {
	X          int32
	Y          int32
	WheelDelta int32
	Flags      int32
}

// HandshakeData 16 bytes
type HandshakeData struct {
	Machine1 uint32
	Machine2 uint32
	Machine3 uint32
	Machine4 uint32
}

// GenericData is the overarching Union basically
type GenericData struct {
	Header Header
	// We'll just define pointers and populate whichever is relevant
	Keyboard  *KeyboardData
	Mouse     *MouseData
	Handshake *HandshakeData
	// raw payload for unknown/clipboard
	Raw []byte
}

func Marshal(data *GenericData) ([]byte, error) {
	buf := make([]byte, PackageSizeEx) // Start with max size, we can trim later if needed
	
	// Header
	binary.LittleEndian.PutUint32(buf[0:4], uint32(data.Header.Type))
	binary.LittleEndian.PutUint32(buf[4:8], data.Header.Id)
	binary.LittleEndian.PutUint32(buf[8:12], data.Header.Src)
	binary.LittleEndian.PutUint32(buf[12:16], data.Header.Des)
	binary.LittleEndian.PutUint64(buf[16:24], data.Header.DateTime)

	// Payload
	switch data.Header.Type {
	case Keyboard:
		if data.Keyboard == nil {
			return nil, errors.New("keyboard data missing")
		}
		// 8 bytes payload, total 32 bytes
		binary.LittleEndian.PutUint32(buf[24:28], uint32(data.Keyboard.Vk))
		binary.LittleEndian.PutUint32(buf[28:32], uint32(data.Keyboard.Flags))
		return buf[:PackageSize], nil

	case Mouse:
		if data.Mouse == nil {
			return nil, errors.New("mouse data missing")
		}
		// 16 bytes payload, total 40 bytes - so we pad to PackageSizeEx (64)
		binary.LittleEndian.PutUint32(buf[24:28], uint32(data.Mouse.X))
		binary.LittleEndian.PutUint32(buf[28:32], uint32(data.Mouse.Y))
		binary.LittleEndian.PutUint32(buf[32:36], uint32(data.Mouse.WheelDelta))
		binary.LittleEndian.PutUint32(buf[36:40], uint32(data.Mouse.Flags))
		return buf[:PackageSizeEx], nil

	case Handshake, HandshakeAck:
		if data.Handshake == nil {
			return nil, errors.New("handshake data missing")
		}
		// 16 bytes payload, total 40 bytes, pad to 64 usually, but maybe 64. Let's return 64.
		binary.LittleEndian.PutUint32(buf[24:28], data.Handshake.Machine1)
		binary.LittleEndian.PutUint32(buf[28:32], data.Handshake.Machine2)
		binary.LittleEndian.PutUint32(buf[32:36], data.Handshake.Machine3)
		binary.LittleEndian.PutUint32(buf[36:40], data.Handshake.Machine4)
		return buf[:PackageSizeEx], nil
	
	default:
		// Heartbeats etc might just be 32 bytes with no specific payload
		if data.Raw != nil {
			copy(buf[24:], data.Raw)
		}
		// for now return the large buffer just to be safe, or 32 if raw is small
		return buf[:PackageSizeEx], nil
	}
}

func Unmarshal(b []byte) (*GenericData, error) {
	if len(b) < 24 {
		return nil, errors.New("buffer too small for header")
	}

	data := &GenericData{}
	data.Header.Type = PackageType(binary.LittleEndian.Uint32(b[0:4]))
	data.Header.Id = binary.LittleEndian.Uint32(b[4:8])
	data.Header.Src = binary.LittleEndian.Uint32(b[8:12])
	data.Header.Des = binary.LittleEndian.Uint32(b[12:16])
	data.Header.DateTime = binary.LittleEndian.Uint64(b[16:24])

	switch data.Header.Type {
	case Keyboard:
		if len(b) >= 32 {
			data.Keyboard = &KeyboardData{
				Vk:    int32(binary.LittleEndian.Uint32(b[24:28])),
				Flags: int32(binary.LittleEndian.Uint32(b[28:32])),
			}
		}
	case Mouse:
		if len(b) >= 40 {
			data.Mouse = &MouseData{
				X:          int32(binary.LittleEndian.Uint32(b[24:28])),
				Y:          int32(binary.LittleEndian.Uint32(b[28:32])),
				WheelDelta: int32(binary.LittleEndian.Uint32(b[32:36])),
				Flags:      int32(binary.LittleEndian.Uint32(b[36:40])),
			}
		}
	case Handshake, HandshakeAck:
		if len(b) >= 40 {
			data.Handshake = &HandshakeData{
				Machine1: binary.LittleEndian.Uint32(b[24:28]),
				Machine2: binary.LittleEndian.Uint32(b[28:32]),
				Machine3: binary.LittleEndian.Uint32(b[32:36]),
				Machine4: binary.LittleEndian.Uint32(b[36:40]),
			}
		}
	default:
		if len(b) > 24 {
			data.Raw = make([]byte, len(b)-24)
			copy(data.Raw, b[24:])
		}
	}

	return data, nil
}
