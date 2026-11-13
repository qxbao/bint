package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash là băm ổn định của phần định nghĩa ảnh hưởng tới việc chạy: schema, tên và các bước (sau Normalize, với
// verify đã quy về giá trị hiệu lực nên vắng mặt và `verify: true` cho cùng băm). Mô tả và chú thích YAML không
// tính, thứ tự khoá trong tệp YAML cũng không (băm lấy từ cấu trúc, không từ văn bản). Dạng "sha256:<hex>".
func Hash(d Def) string {
	type hstep struct {
		ID       string `json:"id"`
		Kind     Kind   `json:"kind"`
		Skill    string `json:"skill"`
		Role     string `json:"role"`
		Optional bool   `json:"optional"`
		Verify   bool   `json:"verify"`
		Fix      string `json:"fix"`
	}
	type hdef struct {
		Schema int     `json:"schema"`
		Name   string  `json:"name"`
		Steps  []hstep `json:"steps"`
	}
	n := Normalize(d)
	h := hdef{Schema: n.Schema, Name: n.Name, Steps: make([]hstep, len(n.Steps))}
	for i, s := range n.Steps {
		h.Steps[i] = hstep{s.ID, s.Kind, s.Skill, s.Role, s.Optional, s.VerifyOn(), s.Fix}
	}
	b, _ := json.Marshal(h) // chỉ gồm chuỗi/số/bool: không thể lỗi
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
