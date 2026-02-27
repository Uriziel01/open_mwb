package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

var InitialIV = "18446744073709551615"

type MWBCrypto struct {
	Key         []byte
	IV          []byte
	MagicNumber uint32
}

func NewMWBCrypto(securityKey string) *MWBCrypto {
	saltUTF16 := stringToUTF16LE(InitialIV)
	key := pbkdf2.Key([]byte(securityKey), saltUTF16, 50000, 32, sha512.New)

	iv := make([]byte, aes.BlockSize)
	copy(iv, []byte(InitialIV[:16]))

	magic := get24BitHash(securityKey)

	return &MWBCrypto{
		Key:         key,
		IV:          iv,
		MagicNumber: magic,
	}
}

func stringToUTF16LE(s string) []byte {
	buf := make([]byte, len(s)*2)
	for i, c := range s {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(c))
	}
	return buf
}

func get24BitHash(key string) uint32 {
	if key == "" {
		return 0
	}

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

type StreamCipher struct {
	encrypter cipher.BlockMode
	decrypter cipher.BlockMode
}

func (c *MWBCrypto) NewStreamCipher(debug bool) *StreamCipher {
	block, err := aes.NewCipher(c.Key)
	if err != nil {
		panic(err)
	}

	encIV := make([]byte, len(c.IV))
	copy(encIV, c.IV)
	decIV := make([]byte, len(c.IV))
	copy(decIV, c.IV)

	return &StreamCipher{
		encrypter: cipher.NewCBCEncrypter(block, encIV),
		decrypter: cipher.NewCBCDecrypter(block, decIV),
	}
}

func (sc *StreamCipher) Encrypt(plaintext []byte) []byte {
	if len(plaintext)%aes.BlockSize != 0 {
		panic(fmt.Sprintf("plaintext length %d must be multiple of %d", len(plaintext), aes.BlockSize))
	}

	ciphertext := make([]byte, len(plaintext))
	sc.encrypter.CryptBlocks(ciphertext, plaintext)
	return ciphertext
}

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

func (sc *StreamCipher) SendRandomBlock(w io.Writer) error {
	ranData := make([]byte, aes.BlockSize)
	if _, err := rand.Read(ranData); err != nil {
		return err
	}

	encrypted := sc.Encrypt(ranData)
	_, err := w.Write(encrypted)
	return err
}

func (sc *StreamCipher) ReceiveRandomBlock(r io.Reader) error {
	buf := make([]byte, aes.BlockSize)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return err
	}

	_ = sc.Decrypt(buf)
	return nil
}
