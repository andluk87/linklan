package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"time"
)

const RelayOverhead = 60

func MAC(key, b []byte) []byte         { m := hmac.New(sha256.New, key); _, _ = m.Write(b); return m.Sum(nil) }
func CheckMAC(key, b, tag []byte) bool { return hmac.Equal(MAC(key, b), tag) }
func WrapRelay(id ID, seq uint64, key, frame []byte) []byte {
	b := make([]byte, RelayOverhead+len(frame))
	copy(b, "LLR1")
	copy(b[4:20], id[:])
	binary.BigEndian.PutUint64(b[20:28], seq)
	copy(b[60:], frame)
	copy(b[28:60], MAC(key, append(append([]byte{}, b[:28]...), frame...)))
	return b
}
func UnwrapRelay(b, key []byte) (ID, uint64, []byte, bool) {
	var id ID
	if len(b) < RelayOverhead+HeaderSize+16 || len(b) > RelayOverhead+MaxFrame || string(b[:4]) != "LLR1" {
		return id, 0, nil, false
	}
	copy(id[:], b[4:20])
	seq := binary.BigEndian.Uint64(b[20:28])
	good := seq > 0 && CheckMAC(key, append(append([]byte{}, b[:28]...), b[60:]...), b[28:60])
	return id, seq, b[60:], good
}
func Cookie(secret []byte, endpoint string, id ID, nonce []byte, bucket int64) []byte {
	b := append([]byte(endpoint), id[:]...)
	b = append(b, nonce...)
	b = binary.BigEndian.AppendUint64(b, uint64(bucket))
	return MAC(secret, b)
}
func VerifyCookie(secret []byte, endpoint string, id ID, nonce, cookie []byte, now time.Time) bool {
	b := now.Unix() / 30
	return hmac.Equal(cookie, Cookie(secret, endpoint, id, nonce, b)) || hmac.Equal(cookie, Cookie(secret, endpoint, id, nonce, b-1))
}
