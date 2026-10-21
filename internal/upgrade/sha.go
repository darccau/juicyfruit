package upgrade

import "crypto/sha256"

func sha256Digest(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
