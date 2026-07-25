package indexer

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256sum returns the hex-encoded SHA-256 digest of s.
// Used as a content-addressed key for module/project summary cache entries.
func sha256sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
