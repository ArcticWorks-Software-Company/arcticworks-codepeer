package githubx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const signaturePrefix = "sha256="

// VerifySignature reports whether the X-Hub-Signature-256 header matches the
// HMAC-SHA256 of body computed with the webhook secret.
func VerifySignature(secret, body []byte, signatureHeader string) bool {
	if signatureHeader == "" || !strings.HasPrefix(signatureHeader, signaturePrefix) {
		return false
	}
	sigHex := strings.TrimPrefix(signatureHeader, signaturePrefix)
	provided, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}
