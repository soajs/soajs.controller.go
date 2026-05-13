package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// KeyConfig holds key encryption configuration
type KeyConfig struct {
	Algorithm string
	Password  string
}

// DefaultKeyConfig returns the default key configuration
func DefaultKeyConfig() KeyConfig {
	return KeyConfig{
		Algorithm: "aes256",
		Password:  "soajs key default",
	}
}

// KeyInfo holds decrypted key information
type KeyInfo struct {
	TenantID    string
	UID         string
	Key         string
	PackageCode string
}

// DecryptKey decrypts an external key and extracts key information
func DecryptKey(extKey string, config KeyConfig) (*KeyInfo, error) {
	if extKey == "" {
		return nil, errors.New("external key is empty")
	}

	if config.Algorithm == "" {
		config.Algorithm = "aes256"
	}
	if config.Password == "" {
		config.Password = "soajs key default"
	}

	// Derive key and IV using EVP key derivation (OpenSSL compatible)
	key, iv, err := evpBytesToKey(config.Algorithm, config.Password)
	if err != nil {
		return nil, err
	}

	// Decode hex string to bytes
	ciphertext, err := hex.DecodeString(extKey)
	if err != nil {
		return nil, errors.New("invalid external key format")
	}

	// AES block size is 16 bytes - ciphertext must be a multiple
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("invalid ciphertext length")
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Decrypt
	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS7 padding
	plaintext = pkcs7Unpad(plaintext)
	if plaintext == nil {
		return nil, errors.New("invalid padding")
	}

	decrypted := string(plaintext)

	// Parse decrypted key
	// Format: tenantId(24) + uId(24) + key(32) + packageLen_packageCode
	// Minimum length: 85
	if len(decrypted) < 85 {
		return nil, errors.New("decrypted key too short")
	}

	info := &KeyInfo{
		TenantID: decrypted[0:24],
		UID:      decrypted[24:48],
		Key:      decrypted[48:80],
	}

	// Parse package code
	packageTxt := decrypted[80:]
	n := strings.Index(packageTxt, "_")
	if n == -1 {
		return nil, errors.New("invalid package format")
	}

	packageCodeLen, err := strconv.Atoi(packageTxt[:n])
	if err != nil {
		return nil, errors.New("invalid package length")
	}

	// Extract package code (note: length check is optional per Node.js code)
	_ = packageCodeLen
	info.PackageCode = packageTxt[n+1:]

	return info, nil
}

// evpBytesToKey implements OpenSSL's EVP_BytesToKey derivation
func evpBytesToKey(algorithm, password string) ([]byte, []byte, error) {
	// For aes256, key length is 32 bytes, IV length is 16 bytes
	var keyLen, ivLen int
	switch algorithm {
	case "aes256", "aes-256-cbc":
		keyLen = 32
		ivLen = 16
	case "aes128", "aes-128-cbc":
		keyLen = 16
		ivLen = 16
	default:
		keyLen = 32
		ivLen = 16
	}

	var key, iv []byte
	var prev []byte

	for len(key) < keyLen || len(iv) < ivLen {
		h := md5.New()
		if prev != nil {
			h.Write(prev)
		}
		h.Write([]byte(password))
		digest := h.Sum(nil)

		// Fill key first
		for len(key) < keyLen && len(digest) > 0 {
			key = append(key, digest[0])
			digest = digest[1:]
		}
		// Then IV
		for len(iv) < ivLen && len(digest) > 0 {
			iv = append(iv, digest[0])
			digest = digest[1:]
		}

		prev = h.Sum(nil)
	}

	return key[:keyLen], iv[:ivLen], nil
}

// pkcs7Unpad removes PKCS7 padding
func pkcs7Unpad(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	padding := int(data[len(data)-1])
	if padding > len(data) || padding > aes.BlockSize {
		return nil
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil
		}
	}
	return data[:len(data)-padding]
}
