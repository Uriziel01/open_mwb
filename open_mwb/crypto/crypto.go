package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"golang.org/x/crypto/pbkdf2"
)

var (
	// InitialIV is ulong.MaxValue.ToString() in C#
	InitialIV = "18446744073709551615"
)

// MWBCrypto holds the derived key and IV for AES-256 CBC.
type MWBCrypto struct {
	Key         []byte
	IV          []byte
	MagicNumber uint32
}

func NewMWBCrypto(securityKey string) *MWBCrypto {
	// Salt uses UTF-16LE encoding (C# Common.GetBytesU — Unicode)
	saltUTF16 := stringToUTF16LE(InitialIV)

	// PBKDF2: 50,000 iterations, SHA512, 32 bytes output for AES-256
	key := pbkdf2.Key([]byte(securityKey), saltUTF16, 50000, 32, sha512.New)

	// IV uses ASCII encoding (C# Common.GetBytes), first 16 chars
	// "18446744073709551615" is 20 chars, take first 16: "1844674407370955"
	iv := make([]byte, aes.BlockSize)
	copy(iv, []byte(InitialIV[:16]))

	// Magic number: 24-bit hash of the security key
	magic := get24BitHash(securityKey)

	return &MWBCrypto{
		Key:         key,
		IV:          iv,
		MagicNumber: magic,
	}
}

// stringToUTF16LE converts a Go string to UTF-16LE bytes, matching C#'s Encoding.Unicode.GetBytes.
func stringToUTF16LE(s string) []byte {
	buf := make([]byte, len(s)*2)
	for i, c := range s {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(c))
	}
	return buf
}

// get24BitHash matches C# Encryption.Get24BitHash: SHA512 iterated 50,001 times.
func get24BitHash(key string) uint32 {
	if key == "" {
		return 0
	}

	// Fill a PACKAGE_SIZE (32) byte buffer with the key chars as bytes
	bytes := make([]byte, 32)
	for i := 0; i < 32 && i < len(key); i++ {
		bytes[i] = key[i]
	}

	hash := sha512.New()
	hash.Write(bytes)
	hashValue := hash.Sum(nil)

	for i := 0; i < 50000; i++ {
		hash.Reset()
		hash.Write(hashValue)
		hashValue = hash.Sum(nil)
	}

	return uint32((int(hashValue[0]) << 23) + (int(hashValue[1]) << 16) + (int(hashValue[len(hashValue)-1]) << 8) + int(hashValue[2]))
}

// StreamCipher maintains CBC state across multiple encrypt/decrypt calls,
// matching C#'s CryptoStream wrapping a NetworkStream.
type StreamCipher struct {
	encrypter cipher.BlockMode
	decrypter cipher.BlockMode
	debug     bool
}

// NewStreamCipher creates a stream cipher that maintains CBC state.
func (c *MWBCrypto) NewStreamCipher(debug bool) *StreamCipher {
	block, err := aes.NewCipher(c.Key)
	if err != nil {
		panic(err)
	}

	// Each direction gets its own CBC state starting from the same IV
	encIV := make([]byte, len(c.IV))
	copy(encIV, c.IV)
	decIV := make([]byte, len(c.IV))
	copy(decIV, c.IV)

	return &StreamCipher{
		encrypter: cipher.NewCBCEncrypter(block, encIV),
		decrypter: cipher.NewCBCDecrypter(block, decIV),
		debug:     debug,
	}
}

// Encrypt encrypts data using the stream's CBC state.
// Data length MUST be a multiple of 16 (AES block size).
// MWB uses PaddingMode.Zeros — packets are already block-aligned (32 or 64 bytes).
func (sc *StreamCipher) Encrypt(plaintext []byte) []byte {
	if len(plaintext)%aes.BlockSize != 0 {
		panic(fmt.Sprintf("plaintext length %d must be a multiple of AES block size", len(plaintext)))
	}

	ciphertext := make([]byte, len(plaintext))
	sc.encrypter.CryptBlocks(ciphertext, plaintext)
	return ciphertext
}

// Decrypt decrypts data using the stream's CBC state.
func (sc *StreamCipher) Decrypt(ciphertext []byte) []byte {
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil
	}
	if len(ciphertext) == 0 {
		return nil
	}

	plaintext := make([]byte, len(ciphertext))
	sc.decrypter.CryptBlocks(plaintext, ciphertext)
	return plaintext
}

// SendRandomBlock sends a 16-byte random block to prime the CBC stream,
// matching C#'s SendOrReceiveARandomDataBlockPerInitialIV(stream, send=true).
// This must be called once before sending any real data.
func (sc *StreamCipher) SendRandomBlock(w io.Writer) error {
	ranData := make([]byte, aes.BlockSize)
	if _, err := rand.Read(ranData); err != nil {
		return err
	}

	encrypted := sc.Encrypt(ranData)
	_, err := w.Write(encrypted)
	if err != nil {
		return fmt.Errorf("failed to send random CBC primer: %w", err)
	}

	if sc.debug {
		log.Printf("[crypto] Sent 16-byte random CBC primer")
	}
	return nil
}

// ReceiveRandomBlock reads a 16-byte random block to prime the CBC stream,
// matching C#'s SendOrReceiveARandomDataBlockPerInitialIV(stream, send=false).
// This must be called once before reading any real data.
func (sc *StreamCipher) ReceiveRandomBlock(r io.Reader) error {
	buf := make([]byte, aes.BlockSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return fmt.Errorf("failed to receive random CBC primer: %w", err)
	}

	_ = sc.Decrypt(buf)

	if sc.debug {
		log.Printf("[crypto] Received 16-byte random CBC primer")
	}
	return nil
}
