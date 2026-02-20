package sqlite

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-id"
	}
	return hex.EncodeToString(b)
}
