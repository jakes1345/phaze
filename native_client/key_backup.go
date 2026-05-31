package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

// KeyBackupPayload matching the TypeScript format.
type KeyBackupPayload struct {
	Ciphertext string `json:"ciphertext"`
	Salt       string `json:"salt"`
	Iterations int    `json:"iterations"`
}

type backupJSON struct {
	Pub string `json:"pub"`
	Sec string `json:"sec"`
}

const (
	pbkdf2Iterations = 200000
	saltBytes        = 16
	ivBytes          = 12
)

// EncryptKeypair derives a key from PIN using PBKDF2-SHA256, then encrypts pub/sec with AES-GCM.
func EncryptKeypair(pub, sec []byte, pin string) (KeyBackupPayload, error) {
	if len(pin) < 4 {
		return KeyBackupPayload{}, errors.New("PIN must be at least 4 characters")
	}

	salt := make([]byte, saltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return KeyBackupPayload{}, err
	}

	// Derive 256-bit AES key from PIN
	key := pbkdf2.Key([]byte(pin), salt, pbkdf2Iterations, 32, sha256.New)

	// Prepare payload JSON
	payload := backupJSON{
		Pub: base64.StdEncoding.EncodeToString(pub),
		Sec: base64.StdEncoding.EncodeToString(sec),
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return KeyBackupPayload{}, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return KeyBackupPayload{}, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return KeyBackupPayload{}, err
	}

	iv := make([]byte, ivBytes)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return KeyBackupPayload{}, err
	}

	ciphertext := gcm.Seal(nil, iv, plaintext, nil)

	// Combined = IV + ciphertext
	combined := make([]byte, len(iv)+len(ciphertext))
	copy(combined, iv)
	copy(combined[len(iv):], ciphertext)

	return KeyBackupPayload{
		Ciphertext: base64.StdEncoding.EncodeToString(combined),
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Iterations: pbkdf2Iterations,
	}, nil
}

// DecryptKeypair decrypts standard KeyBackupPayload using a PIN, returning pub and sec keys.
func DecryptKeypair(ciphertextB64, saltB64 string, iterations int, pin string) (pub []byte, sec []byte, err error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode salt: %w", err)
	}

	combined, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode ciphertext: %w", err)
	}

	if len(combined) < ivBytes+16 { // at least iv + 16 byte gcm tag
		return nil, nil, errors.New("ciphertext too short")
	}

	iv := combined[:ivBytes]
	ct := combined[ivBytes:]

	if iterations <= 0 {
		iterations = pbkdf2Iterations
	}

	key := pbkdf2.Key([]byte(pin), salt, iterations, 32, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	plaintext, err := gcm.Open(nil, iv, ct, nil)
	if err != nil {
		return nil, nil, errors.New("incorrect PIN")
	}

	var payload backupJSON
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	pub, err = base64.StdEncoding.DecodeString(payload.Pub)
	if err != nil || len(pub) != 32 {
		return nil, nil, errors.New("invalid public key in backup")
	}

	sec, err = base64.StdEncoding.DecodeString(payload.Sec)
	if err != nil || len(sec) != 32 {
		return nil, nil, errors.New("invalid private key in backup")
	}

	return pub, sec, nil
}
