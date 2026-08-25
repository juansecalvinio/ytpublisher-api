package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

const prefix = "ytpub_"

func Generate() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
