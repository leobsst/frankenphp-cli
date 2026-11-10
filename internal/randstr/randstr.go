// Package randstr generates random strings for passwords and secrets.
package randstr

import "crypto/rand"

const (
	// Alphanumeric is [A-Za-z0-9].
	Alphanumeric = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// LowerAlphanumeric is [a-z0-9].
	LowerAlphanumeric = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// New returns n characters drawn uniformly from alphabet (at most 256
// characters) with crypto/rand.
func New(alphabet string, n int) string {
	out := make([]byte, 0, n)
	buf := make([]byte, 1)
	// Bytes above limit are rejected, so every character is equally likely
	limit := 256 - 256%len(alphabet)
	for len(out) < n {
		_, _ = rand.Read(buf) // crypto/rand.Read never fails
		if int(buf[0]) < limit {
			out = append(out, alphabet[int(buf[0])%len(alphabet)])
		}
	}
	return string(out)
}
