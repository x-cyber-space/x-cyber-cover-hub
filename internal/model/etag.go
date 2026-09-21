package model

import (
	"crypto/sha256"
	"encoding/hex"
)

// etagOf returns a short, strong ETag for an image body. Hashing the bytes
// rather than the URL matters because the same artwork is reachable from
// several URLs across providers.
func etagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
