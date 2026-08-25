package protocol

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random 16-character hex string.
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
