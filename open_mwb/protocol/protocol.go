package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
)

type PackageType uint32

const (
	Invalid        PackageType = 0
	Cycled         PackageType = 1
	Heartbeat      PackageType = 20
	Heartbeat_ex   PackageType = 99
	Awake          PackageType = 21
	Hello          PackageType = 24
	ByeBye         PackageType = 25
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

// Header: bytes 0-23 of the DATA struct.
// Note: bytes 1-3 of the Type field are used for checksum (byte 1) and
// magic number (bytes 2-3) before sending. The actual PackageType is byte 0 only.
type Header struct {
	Type     PackageType // offset 0, 4 bytes (byte 0 = type, 1 = checksum, 2-3 = magic)
	Id       uint32      // offset 4
	Src      uint32      // offset 8
	Des      uint32      // offset 12
	DateTime uint64      // offset 16  (overlaps with Md/Machine1 in the C# union!)
}

// KeyboardData: 8 bytes at offset 24.
// In the C# union: FieldOffset(sizeof(PackageType) + 3*sizeof(uint) + sizeof(long))
// = 4 + 12 + 8 = 24
type KeyboardData struct {
	Vk    int32
	Flags int32
}

// MouseData: 16 bytes at offset 16!
// In the C# union: FieldOffset(sizeof(PackageType) + 3*sizeof(uint))
// = 4 + 12 = 16.  This OVERLAPS DateTime!
type MouseData struct {
	X          int32
	Y          int32
	WheelDelta int32
	Flags      int32
}

// HandshakeData: 16 bytes at offset 16.
// Machine1 at FieldOffset(sizeof(PackageType) + 3*sizeof(uint)) = 16
type HandshakeData struct {
	Machine1 uint32
	Machine2 uint32
	Machine3 uint32
	Machine4 uint32
}

// GenericData is the Go representation of the C# DATA union.
type GenericData struct {
	Header      Header
	Keyboard    *KeyboardData
	Mouse       *MouseData
	Handshake   *HandshakeData
	MachineName string // 32 bytes at offset 32-63 in big packets
	Raw         []byte
}


// IsBigPackage returns true if this packet type uses 64-byte packets.
// Matches C#'s DATA.IsBigPackage property.
func IsBigPackage(t PackageType) bool {
	switch t {
	case Hello, Awake, Heartbeat, Heartbeat_ex,
		Handshake, HandshakeAck,
		ClipboardPush, Clipboard, ClipboardAsk,
		ClipboardImage, ClipboardText:
		return true
	default:
		return (t & Matrix) == Matrix
	}
}

// Marshal serializes a GenericData into a byte slice matching C#'s DATA.Bytes layout.
func Marshal(data *GenericData, magicNumber uint32, debug bool) ([]byte, error) {
	size := PackageSize
	if IsBigPackage(data.Header.Type) {
		size = PackageSizeEx
	}
	buf := make([]byte, size)

	// Header: offset 0-15 (Type, Id, Src, Des)
	// Byte 0 = actual type, bytes 1-3 will be set below
	buf[0] = byte(data.Header.Type)
	binary.LittleEndian.PutUint32(buf[4:8], data.Header.Id)
	binary.LittleEndian.PutUint32(buf[8:12], data.Header.Src)
	binary.LittleEndian.PutUint32(buf[12:16], data.Header.Des)

	// Payload depends on type
	switch data.Header.Type {
	case Keyboard:
		if data.Keyboard == nil {
			return nil, errors.New("keyboard data missing")
		}
		// DateTime at offset 16
		binary.LittleEndian.PutUint64(buf[16:24], data.Header.DateTime)
		// KeyboardData at offset 24
		binary.LittleEndian.PutUint32(buf[24:28], uint32(data.Keyboard.Vk))
		binary.LittleEndian.PutUint32(buf[28:32], uint32(data.Keyboard.Flags))

	case Mouse:
		if data.Mouse == nil {
			return nil, errors.New("mouse data missing")
		}
		// MouseData at offset 16 (OVERLAPS DateTime!)
		binary.LittleEndian.PutUint32(buf[16:20], uint32(data.Mouse.X))
		binary.LittleEndian.PutUint32(buf[20:24], uint32(data.Mouse.Y))
		binary.LittleEndian.PutUint32(buf[24:28], uint32(data.Mouse.WheelDelta))
		binary.LittleEndian.PutUint32(buf[28:32], uint32(data.Mouse.Flags))

	case Handshake, HandshakeAck:
		if data.Handshake == nil {
			return nil, errors.New("handshake data missing")
		}
		// Machine1-4 at offset 16 (OVERLAPS DateTime!)
		binary.LittleEndian.PutUint32(buf[16:20], data.Handshake.Machine1)
		binary.LittleEndian.PutUint32(buf[20:24], data.Handshake.Machine2)
		binary.LittleEndian.PutUint32(buf[24:28], data.Handshake.Machine3)
		binary.LittleEndian.PutUint32(buf[28:32], data.Handshake.Machine4)

	default:
		// DateTime at offset 16
		binary.LittleEndian.PutUint64(buf[16:24], data.Header.DateTime)
		if data.Raw != nil {
			n := copy(buf[24:], data.Raw)
			_ = n
		}
	}

	// MachineName: 32 bytes at offset 32-63 (only in big packets)
	if size == PackageSizeEx && data.MachineName != "" {
		name := []byte(data.MachineName)
		// Pad with spaces to 32 bytes (matching C# PadRight(32, ' '))
		padded := make([]byte, 32)
		for i := range padded {
			padded[i] = ' '
		}
		copy(padded, name)
		copy(buf[32:64], padded)
	}


	// Set magic number in bytes 2-3 (before checksum calculation)
	buf[3] = byte((magicNumber >> 24) & 0xFF)
	buf[2] = byte((magicNumber >> 16) & 0xFF)

	// Compute checksum over bytes 2..31
	buf[1] = 0
	var checksum byte
	for i := 2; i < PackageSize; i++ {
		checksum += buf[i]
	}
	buf[1] = checksum

	if debug {
		log.Printf("[protocol] SEND type=%d(0x%02X) id=%d src=%d des=%d magic=0x%08X chk=0x%02X size=%d",
			data.Header.Type, buf[0], data.Header.Id, data.Header.Src, data.Header.Des,
			magicNumber, checksum, size)
	}

	return buf, nil
}

// Unmarshal deserializes a byte slice into GenericData, matching C#'s ProcessReceivedDataEx + DATA.
func Unmarshal(b []byte, magicNumber uint32, debug bool) (*GenericData, error) {
	if len(b) < 24 {
		return nil, errors.New("buffer too small for header")
	}

	// Validate magic number (bytes 2-3)
	magic := (int(b[3]) << 24) + (int(b[2]) << 16)
	if magic != int(magicNumber&0xFFFF0000) {
		if debug {
			log.Printf("[protocol] Magic number invalid: got 0x%08X, want 0x%08X", magic, magicNumber&0xFFFF0000)
		}
		return nil, fmt.Errorf("magic number invalid")
	}

	// Validate checksum
	var checksum byte
	for i := 2; i < PackageSize && i < len(b); i++ {
		checksum += b[i]
	}
	if b[1] != checksum {
		if debug {
			log.Printf("[protocol] Checksum invalid: got 0x%02X, computed 0x%02X", b[1], checksum)
		}
		return nil, fmt.Errorf("checksum invalid")
	}

	// Clear bytes 1-3 before parsing (C# does: buf[3] = buf[2] = buf[1] = 0)
	b[1] = 0
	b[2] = 0
	b[3] = 0

	data := &GenericData{}
	// Byte 0 is the actual PackageType
	data.Header.Type = PackageType(b[0])
	data.Header.Id = binary.LittleEndian.Uint32(b[4:8])
	data.Header.Src = binary.LittleEndian.Uint32(b[8:12])
	data.Header.Des = binary.LittleEndian.Uint32(b[12:16])

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
		// MouseData at offset 16 (overlaps DateTime)
		if len(b) >= 32 {
			data.Mouse = &MouseData{
				X:          int32(binary.LittleEndian.Uint32(b[16:20])),
				Y:          int32(binary.LittleEndian.Uint32(b[20:24])),
				WheelDelta: int32(binary.LittleEndian.Uint32(b[24:28])),
				Flags:      int32(binary.LittleEndian.Uint32(b[28:32])),
			}
		}

	case Handshake, HandshakeAck:
		// Machine1-4 at offset 16 (overlaps DateTime)
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

	// MachineName at offset 32-63 (only in big packets)
	if len(b) >= PackageSizeEx {
		nameBytes := b[32:64]
		// Trim trailing spaces and nulls
		end := len(nameBytes)
		for end > 0 && (nameBytes[end-1] == ' ' || nameBytes[end-1] == 0) {
			end--
		}
		if end > 0 {
			data.MachineName = string(nameBytes[:end])
		}
	}

	if debug {
		log.Printf("[protocol] RECV type=%d id=%d src=%d des=%d big=%v name=%q",
			data.Header.Type, data.Header.Id, data.Header.Src, data.Header.Des,
			IsBigPackage(data.Header.Type), data.MachineName)
	}

	return data, nil

}
