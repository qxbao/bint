package cli

import (
	"fmt"
	"io"
	"strings"

	"bint/internal/evolve"
	"bint/internal/hcomp"
)

// printHarnessChanges in các thay đổi thành phần quy trình của đề xuất (H3), kèm bản thay đổi so
// với thành phần hiện có khi có.
func printHarnessChanges(w io.Writer, p *evolve.Proposal) {
	if !p.IsHarness() {
		return
	}
	for _, ch := range p.HarnessChanges {
		fmt.Fprintf(w, "  [%s] %s %s", ch.Op, ch.Kind, ch.ID)
		if ch.Reason != "" {
			fmt.Fprintf(w, " — %s", ch.Reason)
		}
		fmt.Fprintln(w)
		d := ch.Draft
		if d == nil {
			continue
		}
		c := d.Component()
		fmt.Fprintf(w, "      phạm vi:    %s · rủi ro %s\n", d.Scope, hcomp.Risk(c))
		switch ch.Kind {
		case hcomp.KindGuardrail:
			fmt.Fprintf(w, "      luật:       on=%s when=%s unless=%s action=%s\n", d.On, fmtWhen(d.When), fmtUnless(d.Unless), d.Action)
			fmt.Fprintf(w, "      lời nhắc:   %s\n", d.Message)
		case hcomp.KindGuidance:
			if d.Title != "" {
				fmt.Fprintf(w, "      tiêu đề:    %s\n", d.Title)
			}
			fmt.Fprintf(w, "      nội dung:\n%s\n", indent(d.Body, "        "))
		case hcomp.KindScript:
			fmt.Fprintf(w, "      mô tả:      %s\n      cách dùng:  %s\n      script (Bint không bao giờ tự chạy; luôn chờ người duyệt):\n%s\n",
				d.Description, d.Usage, indent(d.Script, "        "))
		case hcomp.KindStrategy:
			if b, err := hcomp.Marshal(c); err == nil {
				fmt.Fprintf(w, "      strategy.yml:\n%s\n", indent(string(b), "        "))
			}
		}
	}
}

func fmtWhen(w hcomp.When) string {
	var parts []string
	if w.Edited != "" {
		parts = append(parts, "edited:"+w.Edited)
	}
	if w.Paths != "" {
		parts = append(parts, "paths:"+w.Paths)
	}
	if w.Command != "" {
		parts = append(parts, "command:"+w.Command)
	}
	if len(parts) == 0 {
		return "(luôn)"
	}
	return strings.Join(parts, ",")
}

func fmtUnless(u hcomp.Unless) string {
	if u.Ran == "" {
		return "(không)"
	}
	return "ran:" + u.Ran
}
