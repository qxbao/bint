// Package ulid sinh ID dạng ULID (26 ký tự Crockford base32): 48 bit thời gian
// mili giây + 80 bit ngẫu nhiên. Sắp xếp được theo thời gian, tạo trên nhiều máy
// không xung đột.
package ulid

import (
	"crypto/rand"
	"time"
)

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New trả về ULID cho thời điểm hiện tại.
func New() string { return At(time.Now()) }

// At trả về ULID cho thời điểm t.
func At(t time.Time) string {
	var b [16]byte
	ms := uint64(t.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(b[6:]); err != nil {
		panic("ulid: crypto/rand: " + err.Error())
	}
	return encode(b)
}

func encode(b [16]byte) string {
	// 128 bit -> 26 ký tự, 5 bit mỗi ký tự, bit cao nhất đệm 2 bit 0.
	var out [26]byte
	var acc uint32
	bits := 2 // 130 - 128
	idx := 0
	for _, by := range b {
		acc = acc<<8 | uint32(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[idx] = alphabet[(acc>>uint(bits))&31]
			idx++
		}
	}
	return string(out[:])
}

// Valid kiểm tra s có phải ULID hợp lệ về định dạng.
func Valid(s string) bool {
	if len(s) != 26 || s[0] > '7' {
		return false
	}
	for i := 0; i < len(s); i++ {
		ok := false
		for j := 0; j < len(alphabet); j++ {
			if s[i] == alphabet[j] {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
