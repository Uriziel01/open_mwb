package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type PackageType uint32

const (
	Hi               PackageType = 2
	Hello            PackageType = 3
	ByeBye           PackageType = 4
	Heartbeat        PackageType = 20
	Awake            PackageType = 21
	HideMouse        PackageType = 50
	Heartbeat_ex     PackageType = 51
	Clipboard        PackageType = 69
	ClipboardDataEnd PackageType = 76
	MachineSwitched  PackageType = 77
	ClipboardAsk     PackageType = 78
	ClipboardPush    PackageType = 79
	NextMachine      PackageType = 121
	Keyboard         PackageType = 122
	Mouse            PackageType = 123
	ClipboardText    PackageType = 124
	ClipboardImage   PackageType = 125
	Handshake        PackageType = 126
	HandshakeAck     PackageType = 127
	Matrix           PackageType = 128
	Invalid          PackageType = 0xFF
	Error            PackageType = 0xFE
)

const (
	PackageSize   = 32
	PackageSizeEx = 64
)

type Header struct {
	Type     PackageType
	Id       uint32
	Src      uint32
	Des      uint32
	DateTime uint64
}

type KeyboardData struct {
	Vk    int32
	Flags int32
}

type MouseData struct {
	X          int32
	Y          int32
	WheelDelta int32
	Flags      int32
}

type HandshakeData struct {
	Machine1 uint32
	Machine2 uint32
	Machine3 uint32
	Machine4 uint32
}

type GenericData struct {
	Header      Header
	Keyboard    *KeyboardData
	Mouse       *MouseData
	Handshake   *HandshakeData
	MachineName string
	Raw         []byte
}

func IsBigPackage(t PackageType) bool {
	switch t {
	case Hello, Awake, Heartbeat, Heartbeat_ex,
		Handshake, HandshakeAck,
		ClipboardPush, Clipboard, ClipboardAsk,
		ClipboardImage, ClipboardText, ClipboardDataEnd:
		return true
	default:
		return (t & Matrix) == Matrix
	}
}

func Marshal(data *GenericData, magicNumber uint32, debug bool) ([]byte, error) {
	size := PackageSize
	if IsBigPackage(data.Header.Type) {
		size = PackageSizeEx
	}
	buf := make([]byte, size)

	buf[0] = byte(data.Header.Type)
	binary.LittleEndian.PutUint32(buf[4:8], data.Header.Id)
	binary.LittleEndian.PutUint32(buf[8:12], data.Header.Src)
	binary.LittleEndian.PutUint32(buf[12:16], data.Header.Des)

	switch data.Header.Type {
	case Keyboard:
		if data.Keyboard == nil {
			return nil, errors.New("keyboard data missing")
		}
		binary.LittleEndian.PutUint64(buf[16:24], data.Header.DateTime)
		binary.LittleEndian.PutUint32(buf[24:28], uint32(data.Keyboard.Vk))
		binary.LittleEndian.PutUint32(buf[28:32], uint32(data.Keyboard.Flags))

	case Mouse:
		if data.Mouse == nil {
			return nil, errors.New("mouse data missing")
		}
		binary.LittleEndian.PutUint32(buf[16:20], uint32(data.Mouse.X))
		binary.LittleEndian.PutUint32(buf[20:24], uint32(data.Mouse.Y))
		binary.LittleEndian.PutUint32(buf[24:28], uint32(data.Mouse.WheelDelta))
		binary.LittleEndian.PutUint32(buf[28:32], uint32(data.Mouse.Flags))

	case Handshake, HandshakeAck:
		if data.Handshake == nil {
			return nil, errors.New("handshake data missing")
		}
		binary.LittleEndian.PutUint32(buf[16:20], data.Handshake.Machine1)
		binary.LittleEndian.PutUint32(buf[20:24], data.Handshake.Machine2)
		binary.LittleEndian.PutUint32(buf[24:28], data.Handshake.Machine3)
		binary.LittleEndian.PutUint32(buf[28:32], data.Handshake.Machine4)

	default:
		binary.LittleEndian.PutUint64(buf[16:24], data.Header.DateTime)
		if data.Raw != nil {
			copy(buf[24:], data.Raw)
		}
	}

	if size == PackageSizeEx && data.MachineName != "" {
		name := []byte(data.MachineName)
		padded := make([]byte, 32)
		for i := range padded {
			padded[i] = ' '
		}
		copy(padded, name)
		copy(buf[32:64], padded)
	}

	buf[3] = byte((magicNumber >> 24) & 0xFF)
	buf[2] = byte((magicNumber >> 16) & 0xFF)

	buf[1] = 0
	var checksum byte
	for i := 2; i < PackageSize; i++ {
		checksum += buf[i]
	}
	buf[1] = checksum

	return buf, nil
}

func Unmarshal(b []byte, magicNumber uint32, debug bool) (*GenericData, error) {
	if len(b) < 24 {
		return nil, errors.New("buffer too small")
	}

	magic := (int(b[3]) << 24) + (int(b[2]) << 16)
	if magic != int(magicNumber&0xFFFF0000) {
		return nil, fmt.Errorf("invalid magic")
	}

	var checksum byte
	for i := 2; i < PackageSize && i < len(b); i++ {
		checksum += b[i]
	}
	if b[1] != checksum {
		return nil, fmt.Errorf("invalid checksum")
	}

	b[1], b[2], b[3] = 0, 0, 0

	data := &GenericData{
		Header: Header{
			Type: PackageType(b[0]),
			Id:   binary.LittleEndian.Uint32(b[4:8]),
			Src:  binary.LittleEndian.Uint32(b[8:12]),
			Des:  binary.LittleEndian.Uint32(b[12:16]),
		},
	}

	switch data.Header.Type {
	case Keyboard:
		data.Header.DateTime = binary.LittleEndian.Uint64(b[16:24])
		if len(b) >= 32 {
			data.Keyboard = &KeyboardData{
				Vk:    int32(binary.LittleEndian.Uint32(b[24:28])),
				Flags: int32(binary.LittleEndian.Uint32(b[28:32])),
			}
		}

	case Mouse:
		if len(b) >= 32 {
			data.Mouse = &MouseData{
				X:          int32(binary.LittleEndian.Uint32(b[16:20])),
				Y:          int32(binary.LittleEndian.Uint32(b[20:24])),
				WheelDelta: int32(binary.LittleEndian.Uint32(b[24:28])),
				Flags:      int32(binary.LittleEndian.Uint32(b[28:32])),
			}
		}

	case Handshake, HandshakeAck:
		if len(b) >= 32 {
			data.Handshake = &HandshakeData{
				Machine1: binary.LittleEndian.Uint32(b[16:20]),
				Machine2: binary.LittleEndian.Uint32(b[20:24]),
				Machine3: binary.LittleEndian.Uint32(b[24:28]),
				Machine4: binary.LittleEndian.Uint32(b[28:32]),
			}
		}

	default:
		data.Header.DateTime = binary.LittleEndian.Uint64(b[16:24])
		if len(b) > 24 {
			data.Raw = make([]byte, len(b)-24)
			copy(data.Raw, b[24:])
		}
	}

	if len(b) >= PackageSizeEx {
		nameBytes := b[32:64]
		end := len(nameBytes)
		for end > 0 && (nameBytes[end-1] == ' ' || nameBytes[end-1] == 0) {
			end--
		}
		if end > 0 {
			data.MachineName = string(nameBytes[:end])
		}
	}

	return data, nil
}
