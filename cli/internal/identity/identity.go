package identity

import (
	"crypto/sha256"
	"encoding/hex"
)

func AccountID(fakeID string) string {
	return "account:" + digest(fakeID)
}

func ArticleID(fakeID, aid string) string {
	return "article:" + digest(fakeID+"\x00"+aid)
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
