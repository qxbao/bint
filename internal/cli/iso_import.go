package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/iso"
)

func newIsoImportCmd(stdio IO) *cobra.Command {
	var fromAI, withSessions, dry, asJSON bool
	var source string
	c := &cobra.Command{
		Use:   "import --from-ai-isolator [<tool>--<tên>...]",
		Short: "Nhập profile từ ai-isolator (đăng nhập, cấu hình, Codex LB) — chỉ người dùng",
		Long: `Tạo profile bint cùng <tool>/<tên> từ store của ai-isolator (AI_ISOLATOR_HOME, mặc định
~/.local/share/ai-isolator của home thật; --source đè). Không có đối số thì nhập mọi profile claude/codex; có
thì chỉ các profile <tool>--<tên> đã nêu (agy chưa hỗ trợ, bị bỏ qua và báo).

Chỉ chép tệp thường trong danh sách cho phép, store ai-isolator không bị đổi:
  Codex   .codex/auth.json, .codex/config.toml, .codex/rules/
  Claude  .claude/.credentials.json, .claude/.claude.json, .claude/settings.json, .claude/CLAUDE.md
  chung   .gitconfig
Tệp được chép với quyền 0600. Codex nhập thêm codex-lb.mode/url làm LB của profile (không gọi mạng).
--with-sessions chép thêm lịch sử phiên (Codex sessions/, archived_sessions/, session_index.jsonl, history.jsonl;
Claude projects/, history.jsonl) — có thể rất lớn, xem trước bằng --dry-run.
Không chép cache/, tmp/, data/, state/, config/ của profile ai-isolator, bản cài (claude versions, codex
packages), thư mục temp/log/sqlite của harness, symlink và dữ liệu người dùng ở gốc HOME.

Sau khi chép, import chạy cùng bước hoàn thiện như create (liên kết cache, bản cài, đồng bộ cấu hình); mục MCP
trùng tên bản thật bị bỏ khỏi cấu hình đã chép để sync dựng lại. Profile bint đã có thì bỏ qua (không ghi đè);
lỗi giữa chừng thì gỡ profile vừa tạo. --dry-run chỉ liệt kê. Thoát 1 khi có profile lỗi; không chạy được từ
trong phiên agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.RequireHuman("bint iso import"); err != nil {
				return err
			}
			if !fromAI {
				return usageError{errors.New("thiếu --from-ai-isolator (nguồn nhập duy nhất hiện có)")}
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			out, err := app.IsoImport(cmd.Context(), st, app.IsoImportOptions{
				Source: source, Only: args, WithSessions: withSessions, DryRun: dry,
			})
			if err != nil {
				return err
			}
			failed := 0
			for _, o := range out {
				if o.Status == iso.ImportFailed {
					failed++
				}
			}
			if asJSON {
				if out == nil {
					out = []app.IsoImportOutcome{}
				}
				if err := writeJSON(stdio.Out, out); err != nil {
					return err
				}
			} else {
				printIsoImport(stdio, out, dry)
			}
			if failed > 0 {
				return fmt.Errorf("%d profile nhập lỗi", failed)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&fromAI, "from-ai-isolator", false, "nhập từ store ai-isolator (bắt buộc)")
	c.Flags().BoolVar(&withSessions, "with-sessions", false, "chép cả lịch sử phiên")
	c.Flags().BoolVar(&dry, "dry-run", false, "chỉ liệt kê, không ghi")
	c.Flags().StringVar(&source, "source", "", "gốc store ai-isolator (đè AI_ISOLATOR_HOME)")
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	return c
}

func printIsoImport(stdio IO, out []app.IsoImportOutcome, dry bool) {
	w := stdio.Out
	if len(out) == 0 {
		fmt.Fprintln(w, "Không có profile claude/codex nào để nhập trong store ai-isolator.")
		return
	}
	for _, o := range out {
		switch o.Status {
		case iso.ImportSkipped:
			fmt.Fprintf(w, "- %s: bỏ qua — %s\n", o.ID, o.Reason)
		case iso.ImportFailed:
			fmt.Fprintf(w, "! %s: lỗi — %s\n", o.ID, o.Reason)
		default:
			verb := "đã nhập"
			if o.Status == iso.ImportPlanned {
				verb = "sẽ nhập"
			}
			cred := ""
			if o.HasCreds {
				cred = ", có tệp đăng nhập"
			}
			fmt.Fprintf(w, "+ %s: %s %d tệp, %s%s\n", o.ID, verb, len(o.Files), humanBytes(o.Bytes), cred)
		}
		if o.Status == iso.ImportImported || o.Status == iso.ImportPlanned {
			printImportGroups(w, o)
			switch o.LB.Mode {
			case iso.LBLinked:
				fmt.Fprintf(w, "    Codex LB linked  %s\n", o.LB.URL)
			case iso.LBDirect:
				fmt.Fprintln(w, "    Codex LB direct")
			}
			for _, s := range o.Skipped {
				switch {
				case s.Count > 0:
					fmt.Fprintf(w, "    bỏ  %s: %d mục\n", s.Name, s.Count)
				default:
					plus := ""
					if s.Capped {
						plus = "+"
					}
					fmt.Fprintf(w, "    bỏ  %s (%s%s)\n", s.Name, humanBytes(s.Bytes), plus)
				}
			}
		}
		for _, n := range o.Notes {
			fmt.Fprintf(w, "    ghi chú: %s\n", n)
		}
		if o.Final != nil {
			printIsoFinalizeResult(stdio, o.Profile, *o.Final)
			fmt.Fprintf(w, "    Tiếp theo: bint iso run %s --name %s\n", o.Profile.Tool, o.Profile.Name)
		}
	}
	if dry {
		fmt.Fprintln(w, "\n(--dry-run: chưa ghi gì)")
	}
}

// printImportGroups in số tệp và dung lượng theo nhóm đã nhập.
func printImportGroups(w io.Writer, o app.IsoImportOutcome) {
	type agg struct {
		n int
		b int64
	}
	by := map[string]*agg{}
	for _, f := range o.Files {
		a := by[f.Group]
		if a == nil {
			a = &agg{}
			by[f.Group] = a
		}
		a.n++
		a.b += f.Bytes
	}
	names := make([]string, 0, len(by))
	for g := range by {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		fmt.Fprintf(w, "    %-10s %d tệp, %s\n", g, by[g].n, humanBytes(by[g].b))
	}
}
