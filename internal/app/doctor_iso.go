package app

import (
	"fmt"
	"os"
	"strings"

	"bint/internal/config"
	"bint/internal/iso"
)

// doctorIso báo tình trạng profile của `bint iso` (docs/iso-plan.md mục 5.5): số profile, tổng dung lượng, temp,
// phiên. Chỉ thêm dòng khi store đã có profile. Mức warn khi HOME giả có dữ liệu người dùng (bint không tự xoá)
// hoặc liên kết chia sẻ xung đột. Chỉ đọc: không tạo thư mục, không dọn gì (việc đó của `bint iso gc`).
func doctorIso(r *DoctorReport) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return // cấu hình toàn cục hỏng đã được doctor báo ở chỗ khác
	}
	st, err := iso.OpenStore(cfg.Iso, os.Environ())
	if err != nil {
		r.add("iso", LevelWarn, "%v", err)
		return
	}
	profiles, lerr := st.List()
	if len(profiles) == 0 && lerr == nil {
		return
	}
	var total, temp, sessions int64
	var issues []string
	for _, p := range profiles {
		u, err := iso.Usage(p, nil)
		if err != nil {
			issues = append(issues, err.Error())
		}
		total += u.Total
		temp += u.Temp
		sessions += u.Sessions
		if len(u.UserEntries) > 0 {
			names := make([]string, len(u.UserEntries))
			for i, e := range u.UserEntries {
				names[i] = e.Path
			}
			issues = append(issues, fmt.Sprintf("%s: dữ liệu người dùng trong HOME giả (%s) — bint không tự xoá", p.ID(), strings.Join(names, ", ")))
		}
		if links, err := iso.CheckShares(p, os.Environ()); err == nil {
			for _, l := range links {
				if l.State.Conflict() {
					issues = append(issues, fmt.Sprintf("%s: liên kết chia sẻ %s %s", p.ID(), l.Link, l.State))
				}
			}
		}
	}
	if lerr != nil {
		issues = append(issues, lerr.Error())
	}
	summary := fmt.Sprintf("%d profile, tổng %s (phiên %s, temp %s) — dọn: bint iso gc", len(profiles), isoBytes(total), isoBytes(sessions), isoBytes(temp))
	if len(issues) > 0 {
		r.add("iso", LevelWarn, "%s\n%s", summary, strings.Join(issues, "\n"))
		return
	}
	r.add("iso", LevelInfo, "%s", summary)
}

// isoBytes in dung lượng theo đơn vị nhị phân.
func isoBytes(b int64) string {
	const k = 1024
	switch {
	case b < k:
		return fmt.Sprintf("%d B", b)
	case b < k*k:
		return fmt.Sprintf("%.0f KiB", float64(b)/k)
	case b < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(b)/(k*k))
	}
	return fmt.Sprintf("%.1f GiB", float64(b)/(k*k*k))
}
