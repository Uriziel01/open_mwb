package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"

	"golang.org/x/crypto/pbkdf2"
)

var (
	InitialIV = []byte("18446744073709551615")
)

type MWBCrypto struct {
	key []byte
	iv  []byte
}

func NewMWBCrypto(securityKey string) *MWBCrypto {
	// 50,000 iterations, SHA512, 32 output bytes for AES-256
	key := pbkdf2.Key([]byte(securityKey), InitialIV, 50000, 32, sha512.New)

	// IV is the first 16 bytes of InitialIV string, padded with spaces if needed
	iv := make([]byte, 16)
	copy(iv, InitialIV)
	for i := len(InitialIV); i < 16; i++ {
		iv[i] = ' '
	}

	return &MWBCrypto{
		key: key,
		iv:  iv,
	}
}

// pkcs7Pad adds PKCS7 padding to a byte slice
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

// pkcs7Unpad removes PKCS7 padding from a byte slice
func pkcs7Unpad(data []byte) []byte {
	length := len(data)
	if length == 0 {
		return data
	}
	unpadding := int(data[length-1])
	if unpadding > length {
		return data
	}
	return data[:(length - unpadding)]
}

func (c *MWBCrypto) Encrypt(plaintext []byte) []byte {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		panic(err)
	}

	paddedPlaintext := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(paddedPlaintext))

	mode := cipher.NewCBCEncrypter(block, c.iv)
	mode.CryptBlocks(ciphertext, paddedPlaintext)

	return ciphertext
}

func (c *MWBCrypto) Decrypt(ciphertext []byte) []byte {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		panic(err)
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		panic("ciphertext is not a multiple of the block size")
	}

	if len(ciphertext) == 0 {
		return nil
	}

	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, c.iv)
	mode.CryptBlocks(plaintext, ciphertext)

	return pkcs7Unpad(plaintext)
}
