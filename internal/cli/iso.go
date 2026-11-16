package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/iso"
)

// bint iso: profile tài khoản/cấu hình tách biệt cho harness (docs/iso-plan.md). Logic nằm ở internal/iso;
// tệp này chỉ phân tích cờ, nạp cấu hình toàn cục, in kết quả và đổi lỗi thành mã thoát.

func newIsoCmd(stdio IO) *cobra.Command {
	c := &cobra.Command{
		Use:   "iso",
		Short: "Profile tài khoản/cấu hình tách biệt cho harness (claude, codex)",
		Long: `Mỗi profile là một HOME giả riêng (đăng nhập, cấu hình, lịch sử phiên của harness) nên chạy được
nhiều tài khoản song song. Cache gói dùng chung với máy thật, thư mục temp nằm ở temp hệ thống và được dọn khi
hết phiên; bint trong profile vẫn dùng đúng state/config của máy.

Không phải sandbox: tiến trình con chạy cùng UID, quyền tệp không đổi.

  bint iso create codex --name work     tạo profile (Codex LB tự dò, --no-lb để tắt)
  bint iso login  codex --name work     đăng nhập trong profile
  bint iso run    codex --name work -- exec "..."
  bint iso sync                         đồng bộ skill, MCP, trust và hook từ máy thật (--check: chỉ kiểm)
  bint iso import --from-ai-isolator    nhập profile từ ai-isolator (đăng nhập, cấu hình, LB; --with-sessions: cả lịch sử)
  bint iso shell-init zsh               in hàm codex-work, claude-… (eval "$(bint iso shell-init zsh)")`,
	}
	c.AddCommand(newIsoCreateCmd(stdio), newIsoLoginCmd(stdio), newIsoRunCmd(stdio), newIsoStatusCmd(stdio),
		newIsoDuCmd(stdio), newIsoGCCmd(stdio), newIsoSyncCmd(stdio), newIsoRemoveCmd(stdio), newIsoImportCmd(stdio), newIsoShellInitCmd(stdio))
	return c
}

// openIsoStore nạp cấu hình toàn cục (iso là cài đặt của máy, không phải của repo) và mở store.
func openIsoStore() (*iso.Store, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return nil, err
	}
	return iso.OpenStore(cfg.Iso, os.Environ())
}

// isoTarget phân tích <tool> và --name; sai cú pháp là lỗi dùng (mã 2).
func isoTarget(tool, name string) (iso.Tool, string, error) {
	t, err := iso.ParseTool(tool)
	if err != nil {
		return "", "", usageError{err}
	}
	if err := iso.ValidName(name); err != nil {
		return "", "", usageError{err}
	}
	return t, name, nil
}

// loadIsoProfile nạp profile đã có; chưa có thì báo đúng lệnh create (run không tự tạo: tránh tạo profile do gõ sai tên).
func loadIsoProfile(st *iso.Store, t iso.Tool, name string) (iso.Profile, error) {
	return st.LoadHinted(t, name)
}

// lbFlags là cặp cờ --lb/--no-lb dùng chung của create và run.
type lbFlags struct {
	url  string
	none bool
}

func (f *lbFlags) add(c *cobra.Command) {
	c.Flags().StringVar(&f.url, "lb", "", "URL Codex LB (chỉ codex): chốt chế độ linked; không sẵn sàng thì thoát mã 69")
	c.Flags().BoolVar(&f.none, "no-lb", false, "không dùng Codex LB (chỉ codex): chốt chế độ direct")
}

func (f lbFlags) set() bool { return f.url != "" || f.none }

func (f lbFlags) validate(t iso.Tool) error {
	switch {
	case f.url != "" && f.none:
		return usageError{errors.New("--lb và --no-lb không dùng cùng nhau")}
	case f.set() && t != iso.Codex:
		return usageError{fmt.Errorf("--lb/--no-lb chỉ dùng cho codex, không phải %s", t)}
	}
	return nil
}

// resolve chốt LB theo cờ: --no-lb là direct; ngược lại dò (ResolveLB). LB chỉ định tường minh mà không sẵn sàng
// là lỗi mã 69; tự dò không thấy thì direct.
func (f lbFlags) resolve(cmd *cobra.Command) (iso.LB, string, error) {
	if f.none {
		return iso.LB{Mode: iso.LBDirect}, "no-lb", nil
	}
	lb, src, err := iso.ResolveLB(cmd.Context(), iso.LBOptions{Flag: f.url})
	if err != nil {
		if errors.Is(err, iso.ErrLBUnavailable) {
			return iso.LB{}, src, withCode(ExitUnavailable, err)
		}
		return iso.LB{}, src, usageError{err}
	}
	if lb.Mode == "" {
		return iso.LB{Mode: iso.LBDirect}, "", nil
	}
	return lb, src, nil
}

func newIsoCreateCmd(stdio IO) *cobra.Command {
	var name string
	var lbf lbFlags
	c := &cobra.Command{
		Use:   "create <claude|codex>",
		Short: "Tạo profile mới",
		Long: `Tạo profile <tool>/<name> (mặc định tên "default"). Profile đã có thì báo lỗi, không ghi đè.

Codex: chốt chế độ Codex LB một lần và lưu vào profile — linked (đi qua LB, URL lưu lại và các lần chạy sau
không dò nữa) hoặc direct. URL tìm theo thứ tự --lb, BINT_ISO_CODEX_LB_URL, base_url …/backend-api/codex trong
~/.codex/config.toml thật, http://127.0.0.1:2455; mỗi nguồn được kiểm GET /health/ready. --lb/biến môi trường mà
LB không sẵn sàng thì thoát mã 69; tự dò không thấy thì chọn direct. --no-lb ép direct.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, name, err := isoTarget(args[0], name)
			if err != nil {
				return err
			}
			if err := lbf.validate(t); err != nil {
				return err
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			var lb iso.LB
			src := ""
			if t == iso.Codex {
				// Dò LB trước khi tạo gì: LB tường minh hỏng thì không để lại profile nửa vời.
				if lb, src, err = lbf.resolve(cmd); err != nil {
					return err
				}
			}
			p, err := st.Create(t, name)
			if err != nil {
				if errors.Is(err, iso.ErrExists) {
					return fmt.Errorf("%w (đăng nhập: bint iso login %s --name %s)", err, t, name)
				}
				return err
			}
			if t == iso.Codex {
				p.Meta.LB = lb
				if err := p.SaveMeta(); err != nil {
					_ = iso.RemoveProfile(p)
					return err
				}
			}
			w := stdio.Out
			fmt.Fprintf(w, "Đã tạo profile %s\n  thư mục  %s\n", p.ID(), p.Dir)
			if t == iso.Codex {
				if lb.Mode == iso.LBLinked {
					fmt.Fprintf(w, "  Codex LB linked  %s (nguồn: %s)\n", lb.URL, src)
				} else {
					fmt.Fprintln(w, "  Codex LB direct (không dùng LB)")
				}
			}
			printIsoFinalize(cmd.Context(), stdio, p)
			next := fmt.Sprintf("bint iso login %s --name %s", t, name)
			if lb.Mode == iso.LBLinked {
				next = fmt.Sprintf("bint iso run %s --name %s", t, name)
			}
			fmt.Fprintf(w, "\nTiếp theo: %s\n", next)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", iso.DefaultName, "tên profile")
	lbf.add(c)
	return c
}

// isoHarnessArgs trả đối số dành cho harness: mọi thứ sau <tool>, giữ nguyên (cobra đã bỏ "--").
func isoHarnessArgs(cmd *cobra.Command, args []string) ([]string, error) {
	if len(args) == 0 || cmd.ArgsLenAtDash() == 0 {
		return nil, usageError{fmt.Errorf("thiếu <tool> (%s)", toolList())}
	}
	return args[1:], nil
}

func toolList() string {
	var s []string
	for _, t := range iso.Tools() {
		s = append(s, string(t))
	}
	return strings.Join(s, "|")
}

func newIsoLoginCmd(stdio IO) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "login <claude|codex> [--name N] [-- ĐỐI_SỐ…]",
		Short: "Đăng nhập harness trong profile",
		Long: `Claude: chạy ` + "`claude auth login`" + ` trong profile. Codex direct: chạy ` + "`codex login`" + `. Codex linked: không chạy
harness (đăng nhập do Codex LB và CODEX_LB_API_KEY đảm nhiệm). Đối số sau -- được chuyển cho lệnh đăng nhập.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			extra, err := isoHarnessArgs(cmd, args)
			if err != nil {
				return err
			}
			t, name, err := isoTarget(args[0], name)
			if err != nil {
				return err
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			p, err := loadIsoProfile(st, t, name)
			if err != nil {
				return err
			}
			var login []string
			switch t {
			case iso.Claude:
				login = []string{"auth", "login"}
			default:
				if p.Meta.LB.Mode == iso.LBLinked {
					fmt.Fprintf(stdio.Out, "Profile %s ở chế độ linked (%s): đăng nhập do Codex LB đảm nhiệm, không cần `codex login`.\n"+
						"Đặt CODEX_LB_API_KEY nếu LB đòi khoá, rồi: bint iso run %s --name %s\n", p.ID(), p.Meta.LB.URL, t, name)
					return nil
				}
				login = []string{"login"}
			}
			return runIso(stdio, p, append(login, extra...))
		},
	}
	c.Flags().StringVar(&name, "name", iso.DefaultName, "tên profile")
	return c
}

func newIsoRunCmd(stdio IO) *cobra.Command {
	var name string
	var lbf lbFlags
	c := &cobra.Command{
		Use:   "run <claude|codex> [--name N] [--lb URL|--no-lb] [-- ĐỐI_SỐ…]",
		Short: "Chạy harness trong profile",
		Long: `Chạy harness (tìm trên PATH thật, bỏ thư mục của store) với env của profile: HOME giả, XDG, TMPDIR riêng
(đã tạo sẵn), CODEX_HOME/CLAUDE_CONFIG_DIR, BINT_STATE_DIR/BINT_CONFIG_HOME ghim về của máy. Đối số sau tên tool
được chuyển nguyên văn; cờ của harness đặt sau --. Mã thoát của harness được trả lại (chết vì tín hiệu n: 128+n).

Profile chưa có thì báo lệnh create, không tự tạo. Codex linked chèn bộ cờ -c model_provider=codex-lb trước đối số
của bạn và thoát mã 69 khi LB không sẵn sàng; --lb URL / --no-lb chốt lại chế độ của profile.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			harnessArgs, err := isoHarnessArgs(cmd, args)
			if err != nil {
				return err
			}
			t, name, err := isoTarget(args[0], name)
			if err != nil {
				return err
			}
			if err := lbf.validate(t); err != nil {
				return err
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			p, err := loadIsoProfile(st, t, name)
			if err != nil {
				return err
			}
			if lbf.set() {
				lb, _, err := lbf.resolve(cmd)
				if err != nil {
					return err
				}
				if lb != p.Meta.LB {
					p.Meta.LB = lb
					if err := p.SaveMeta(); err != nil {
						return err
					}
				}
			}
			lbArgs, err := iso.HarnessArgs(cmd.Context(), p, os.Environ())
			if err != nil {
				return withCode(ExitUnavailable, err)
			}
			harnessArgs = append(lbArgs, harnessArgs...)
			warnIsoSyncDrift(cmd.Context(), stdio, p)
			return runIso(stdio, p, harnessArgs)
		},
	}
	c.Flags().StringVar(&name, "name", iso.DefaultName, "tên profile")
	lbf.add(c)
	return c
}

// runIso chạy harness trong profile và đổi mã thoát của con thành mã thoát của bint (không in "lỗi:").
func runIso(stdio IO, p iso.Profile, args []string) error {
	code, err := iso.Run(p, iso.Spec{
		Args: args, Stdin: stdio.In, Stdout: stdio.Out, Stderr: stdio.Err,
		Logf: func(format string, a ...any) { fmt.Fprintf(stdio.Err, "bint iso: "+format+"\n", a...) },
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return quietExit(code)
	}
	return nil
}

func newIsoStatusCmd(stdio IO) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Liệt kê profile: đăng nhập, chế độ LB, phiên đang chạy, thư mục temp",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			rows, serr := iso.Status(st, nil)
			if asJSON {
				if rows == nil {
					rows = []iso.ProfileStatus{}
				}
				if err := writeJSON(stdio.Out, rows); err != nil {
					return err
				}
				return serr
			}
			w := stdio.Out
			fmt.Fprintf(w, "Store: %s\n", st.Root)
			if len(rows) == 0 {
				fmt.Fprintln(w, "Chưa có profile (bint iso create <claude|codex> --name N).")
			} else {
				tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "PROFILE\tĐĂNG NHẬP\tLB\tPHIÊN\tTỔNG\tTEMP\tBINARY")
				for _, r := range rows {
					lb := "-"
					switch r.LBMode {
					case iso.LBLinked:
						lb = "linked " + r.LBURL
					case iso.LBDirect:
						lb = "direct"
					}
					temp := "-"
					if r.TempExists {
						temp = r.Temp
					}
					bin := r.Binary
					if bin == "" {
						bin = "(không thấy trên PATH)"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, isoYesNo(r.Auth, "có", "chưa"), lb,
						isoYesNo(r.Session, "đang chạy", "-"), humanBytes(r.TotalBytes), temp, bin)
				}
				tw.Flush()
				for _, r := range rows {
					for _, is := range r.LinkIssues {
						fmt.Fprintf(w, "! %s: liên kết chia sẻ %s\n", r.ID, is)
					}
					for _, is := range r.Install {
						fmt.Fprintf(w, "! %s: bản cài: %s\n", r.ID, is)
					}
					if line := userDataLine(r.ID, r.UserData); line != "" {
						fmt.Fprintln(w, line)
					}
					if r.DaemonAlive {
						fmt.Fprintf(w, "i %s: daemon app-server Codex đang chạy (temp được giữ, chỉ xoá mục cũ)\n", r.ID)
					}
				}
			}
			return serr
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	return c
}

func isoYesNo(b bool, y, n string) string {
	if b {
		return y
	}
	return n
}

// humanBytes in dung lượng theo đơn vị nhị phân (KiB, MiB, GiB), một chữ số thập phân từ MiB trở lên.
func humanBytes(b int64) string {
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

// userDataLine là dòng cảnh báo dữ liệu người dùng trong HOME giả ("" khi không có).
func userDataLine(id string, entries []iso.UserEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%s (%s)", e.Path, humanBytes(e.Bytes))
	}
	return fmt.Sprintf("! %s: dữ liệu người dùng trong HOME giả: %s — bint không tự xoá", id, strings.Join(parts, ", "))
}

func newIsoDuCmd(stdio IO) *cobra.Command {
	var asJSON bool
	var only string
	c := &cobra.Command{
		Use:   "du",
		Short: "Dung lượng từng profile: auth/config, phiên, cache riêng, bản cài, temp, khác, dữ liệu người dùng",
		Long: `Đo dung lượng đĩa (số khối như du; hardlink đếm một lần; không đi qua liên kết chia sẻ nên cache dùng chung
không bị tính) của mọi profile hoặc một profile theo --profile <tool>/<name>. TỔNG là cả thư mục profile (bằng
du -s thư mục đó), TEMP nằm ngoài thư mục profile nên đo riêng. DỮ LIỆU NGƯỜI DÙNG là mục ở gốc HOME giả không
bắt đầu bằng dấu chấm và không thuộc danh mục cache (vd Projects/): bint không bao giờ tự xoá. --json in byte.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if only != "" {
				if _, _, err := iso.ParseID(only); err != nil {
					return usageError{err}
				}
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			rows, uerr := iso.UsageAll(st, nil, only)
			if asJSON {
				if rows == nil {
					rows = []iso.ProfileUsage{}
				}
				if err := writeJSON(stdio.Out, rows); err != nil {
					return err
				}
				return uerr
			}
			w := stdio.Out
			if len(rows) == 0 {
				fmt.Fprintln(w, "Chưa có profile (bint iso create <claude|codex> --name N).")
				return uerr
			}
			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "PROFILE\tTỔNG\tAUTH\tPHIÊN\tCACHE RIÊNG\tBẢN CÀI\tTEMP\tKHÁC\tDỮ LIỆU NGƯỜI DÙNG")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, humanBytes(r.Total), humanBytes(r.AuthConfig),
					humanBytes(r.Sessions), humanBytes(r.Cache), humanBytes(r.Install), humanBytes(r.Temp),
					humanBytes(r.Other), humanBytes(r.UserData))
			}
			tw.Flush()
			for _, r := range rows {
				if line := userDataLine(r.ID, r.UserEntries); line != "" {
					fmt.Fprintln(w, line)
				}
			}
			return uerr
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON (byte)")
	c.Flags().StringVar(&only, "profile", "", "chỉ một profile, dạng <tool>/<name>")
	return c
}

// isoGCMaxLines là số dòng tối đa in cho một nhóm việc của gc (cache có thể hàng nghìn tệp); --json in đủ.
const isoGCMaxLines = 200

func newIsoGCCmd(stdio IO) *cobra.Command {
	var dry, deep, asJSON bool
	var only string
	c := &cobra.Command{
		Use:   "gc",
		Short: "Dọn temp, cache riêng và lịch sử phiên cũ của profile (--deep: cả bản cài thừa)",
		Long: `Dọn từng profile (hoặc một profile theo --profile <tool>/<name>), mỗi hành động một dòng:
  - temp: xoá thư mục temp khi không còn phiên (còn daemon Codex thì chỉ mục cũ hơn iso.tmp_ttl); temp mồ côi
    của profile không còn trong store cũng bị xoá khi cũ hơn tmp_ttl;
  - cache riêng (thư mục thật trong HOME giả, không phải liên kết chia sẻ): tệp cũ hơn iso.cache_ttl;
  - phiên: Codex sessions/archived_sessions rollout-*.jsonl, Claude projects/*/<id>.jsonl cũ hơn iso.sessions_ttl
    (0 = không xoá), trừ phiên mà tác vụ chưa kết thúc còn tham chiếu (last_step.session_id).
--deep thêm bản cài thừa (claude/versions/* trừ bản đang dùng, codex packages/*/releases/* trừ current khi
packages không phải liên kết) và .claude/projects/<slug> của thư mục nguồn không còn; chỉ người dùng chạy được.
Profile đang có phiên thì bị bỏ qua; còn daemon Codex thì bỏ qua phiên và bản cài. Không đi qua liên kết chia sẻ
và không bao giờ chạm dữ liệu người dùng ở gốc HOME giả. --dry-run chỉ liệt kê đúng danh sách sẽ xoá.
Giới hạn: tác vụ của repo đặt state.dir tuỳ biến (ngoài thư mục trạng thái mặc định) không được thấy khi tính phiên cần giữ.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deep {
				if err := app.RequireHuman("bint iso gc --deep"); err != nil {
					return err
				}
			}
			if only != "" {
				if _, _, err := iso.ParseID(only); err != nil {
					return usageError{err}
				}
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			keep, kerr := iso.OpenTaskSessions(os.Environ())
			if kerr != nil {
				fmt.Fprintf(stdio.Err, "cảnh báo: %v — giữ mọi phiên\n", kerr)
				keep = func(string) bool { return true }
			}
			rep, gerr := iso.GC(st, iso.GCOptions{DryRun: dry, Deep: deep, Only: only, KeepSession: keep})
			if asJSON {
				if rep.Temp == nil {
					rep.Temp = []iso.TempAction{}
				}
				if rep.Actions == nil {
					rep.Actions = []iso.GCAction{}
				}
				if err := writeJSON(stdio.Out, rep); err != nil {
					return err
				}
				return gerr
			}
			printIsoGC(stdio.Out, rep, dry)
			return gerr
		},
	}
	c.Flags().BoolVar(&dry, "dry-run", false, "chỉ liệt kê, không xoá")
	c.Flags().BoolVar(&deep, "deep", false, "thêm bản cài harness thừa và thư mục dự án Claude mồ côi (chỉ người dùng)")
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	c.Flags().StringVar(&only, "profile", "", "chỉ một profile, dạng <tool>/<name>")
	return c
}

func printIsoGC(w io.Writer, rep iso.GCReport, dry bool) {
	will := ""
	if dry {
		will = "sẽ "
	}
	busy := map[string]bool{}
	for _, a := range rep.Actions {
		if a.Kind == iso.GCBusy {
			busy[a.Profile] = true
		}
	}
	lines := 0
	for _, a := range rep.Temp {
		if a.Kind == iso.TempBusy && busy[a.Profile] {
			continue
		}
		verb := map[string]string{
			iso.TempRemoveAll: "xoá", iso.TempPrune: "xoá mục cũ", iso.TempOrphan: "xoá (mồ côi)",
			iso.TempBusy: "bỏ qua (đang có phiên)",
		}[a.Kind]
		if dry && a.Kind != iso.TempBusy {
			verb = "sẽ " + verb
		}
		fmt.Fprintf(w, "%s: %s temp %s", a.Profile, verb, a.Path)
		if len(a.Items) > 0 {
			fmt.Fprintf(w, " [%s]", strings.Join(a.Items, ", "))
		}
		fmt.Fprintln(w)
		lines++
	}
	shown := map[string]int{}
	hidden := 0
	for _, a := range rep.Actions {
		lines++
		switch a.Kind {
		case iso.GCBusy:
			fmt.Fprintf(w, "%s: bỏ qua (%s)\n", a.Profile, a.Note)
			continue
		case iso.GCSkip:
			fmt.Fprintf(w, "%s: %s\n", a.Profile, a.Note)
			continue
		}
		key := a.Profile + "/" + a.Kind
		if shown[key]++; shown[key] > isoGCMaxLines {
			hidden++
			continue
		}
		fmt.Fprintf(w, "%s: %sxoá %s %s (%s)\n", a.Profile, will, a.Kind, a.Path, humanBytes(a.Bytes))
	}
	if hidden > 0 {
		fmt.Fprintf(w, "… và %d mục khác (dùng --json để xem đủ)\n", hidden)
	}
	if lines == 0 {
		fmt.Fprintln(w, "Không có gì cần dọn.")
		return
	}
	if dry {
		fmt.Fprintf(w, "Sẽ giải phóng %s trong profile (chưa tính temp).\n", humanBytes(rep.Freed()))
	} else {
		fmt.Fprintf(w, "Đã giải phóng %s trong profile (chưa tính temp).\n", humanBytes(rep.Freed()))
	}
}

func newIsoRemoveCmd(stdio IO) *cobra.Command {
	var name string
	var yes bool
	c := &cobra.Command{
		Use:   "remove <claude|codex>",
		Short: "Xoá profile (cả đăng nhập và lịch sử phiên) — chỉ người dùng",
		Long: `Xoá profile và thư mục temp của nó. Liên kết chia sẻ cache chỉ bị gỡ, dữ liệu thật ở đích không bị chạm.
Từ chối khi profile đang có phiên hoặc còn daemon Codex. Không chạy được từ trong phiên agent. Cần --yes khi
không chạy trên terminal tương tác.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.RequireHuman("bint iso remove"); err != nil {
				return err
			}
			t, name, err := isoTarget(args[0], name)
			if err != nil {
				return err
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			p, err := loadIsoProfile(st, t, name)
			if err != nil {
				return err
			}
			if !yes {
				if !isTerminal(stdio.In) {
					return usageError{errors.New("xoá profile cần xác nhận: thêm --yes khi không chạy trên terminal tương tác")}
				}
				if !confirm(stdio, fmt.Sprintf("Xoá profile %s (gồm đăng nhập và lịch sử phiên)?", p.ID())) {
					fmt.Fprintln(stdio.Out, "Đã huỷ.")
					return nil
				}
			}
			if err := iso.RemoveProfile(p); err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "Đã xoá profile %s\n", p.ID())
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", iso.DefaultName, "tên profile")
	c.Flags().BoolVar(&yes, "yes", false, "không hỏi xác nhận")
	return c
}

func newIsoShellInitCmd(stdio IO) *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init <" + strings.Join(iso.Shells, "|") + ">",
		Short: "In hàm shell cho từng profile (không ghi tệp nào)",
		Long: `In mỗi profile một hàm <tool>-<name> chạy ` + "`bint iso run <tool> --name <name> -- …`" + `. Thêm vào RC của shell:
  eval "$(bint iso shell-init zsh)"        # bash, zsh
  bint iso shell-init fish | source         # fish
  bint iso shell-init pwsh | Out-String | Invoke-Expression
Bint không tự sửa RC.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			profiles, lerr := st.List()
			out, err := iso.ShellInit(args[0], profiles)
			if err != nil {
				return usageError{err}
			}
			if lerr != nil {
				fmt.Fprintf(stdio.Err, "cảnh báo: bỏ qua profile hỏng: %v\n", lerr)
			}
			_, err = fmt.Fprint(stdio.Out, out)
			return err
		},
	}
}

// isoSyncVerbs: nhãn hành động của sync khi ghi / khi --check.
var isoSyncVerbs = map[iso.Action][2]string{
	iso.ActWrite:    {"đã ghi", "sẽ ghi"},
	iso.ActUpdate:   {"đã cập nhật", "sẽ cập nhật"},
	iso.ActRemove:   {"đã gỡ", "sẽ gỡ"},
	iso.ActConflict: {"XUNG ĐỘT", "XUNG ĐỘT"},
	iso.ActForget:   {"dọn receipt", "dọn receipt"},
	iso.ActSkip:     {"giữ", "giữ"},
	iso.ActError:    {"LỖI", "LỖI"},
}

func isoSyncLabel(rep iso.SyncReport, it iso.SyncItem) string {
	v := isoSyncVerbs[it.Action][0]
	if rep.Check {
		v = isoSyncVerbs[it.Action][1]
	}
	what := it.Key
	if it.Kind == iso.KindLink {
		what = "liên kết " + it.File + " → " + it.Key
	} else if it.File != "" {
		what = fmt.Sprintf("%s (%s)", it.Key, it.File)
	}
	if it.Detail != "" {
		what += ": " + it.Detail
	}
	return v + " " + what
}

// printIsoSyncReport in báo cáo sync của một profile: chỉ các mục khác "khớp", rồi ghi chú và tổng kết.
func printIsoSyncReport(w io.Writer, rep iso.SyncReport) {
	ok := 0
	var lines []string
	for _, it := range rep.Items {
		if it.Action == iso.ActKeep {
			ok++
			continue
		}
		lines = append(lines, "  "+isoSyncLabel(rep, it))
	}
	fmt.Fprintf(w, "%s: %d mục khớp bản thật", rep.Profile, ok)
	if len(lines) == 0 {
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, ", %d cần chú ý\n", len(lines))
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	for _, n := range rep.Notes {
		fmt.Fprintf(w, "  ! %s\n", n)
	}
	if len(rep.Conflicts()) > 0 {
		fmt.Fprintf(w, "  (xung đột: sửa hoặc xoá khoá đó ở profile rồi chạy lại bint iso sync; bint không ghi đè)\n")
	}
}

// isoSyncOut là một phần tử của --json.
type isoSyncOut struct {
	iso.SyncReport
	Error string `json:"error,omitempty"`
}

func newIsoSyncCmd(stdio IO) *cobra.Command {
	var check, all, asJSON bool
	var only string
	c := &cobra.Command{
		Use:   "sync [--check] [--profile <tool>/<name> | --all]",
		Short: "Đồng bộ skill, MCP, trust và hook từ máy thật sang profile",
		Long: `Đồng bộ từ cấu hình thật của máy sang từng profile (mặc định mọi profile; --profile <tool>/<name> chỉ một):
  - liên kết skills/agents/commands (claude), skills (codex) và ~/.agents/skills tới bản thật, khi profile chưa có
    thư mục riêng (có thư mục riêng có dữ liệu thì giữ và báo);
  - MCP: [mcp_servers.*] của config.toml Codex thật và mcpServers của .claude.json Claude thật; mục stdio chạy
    bint mang bảng env BINT_STATE_DIR/BINT_CONFIG_HOME của máy thật (nếu không bint mcp mở state giả);
  - Codex: [projects.*] trust_level của gốc repo (thư mục hiện tại), gốc Git main và thư mục worktree của bint,
    [hooks.state.*] của hook dự án trong đó, rồi xác minh bằng hooks/list với env của profile;
  - hook cấp user của bint (bint init --hooks --scope user) cài vào profile, kèm trusted_hash của hook user Codex
    khi currentHash khớp.
Không bao giờ chép auth.json, .credentials.json hay khoá tài khoản của .claude.json. Khoá do sync quản lý ghi trong
sync-receipt.json (sha256); khoá bị sửa tay ở profile là XUNG ĐỘT: sync báo tên khoá và tệp, không ghi đè.

Ghi cần profile không có phiên đang chạy (ErrBusy). --check không ghi gì và không chạy harness: thoát 1 khi lệch
hoặc xung đột, 0 khi khớp. Việc hook user Codex chưa có trusted_hash chỉ xác định được khi ghi (cần hooks/list).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && only != "" {
				return usageError{errors.New("--all và --profile không dùng cùng nhau")}
			}
			st, err := openIsoStore()
			if err != nil {
				return err
			}
			var profiles []iso.Profile
			if only != "" {
				t, name, err := iso.ParseID(only)
				if err != nil {
					return usageError{err}
				}
				p, err := loadIsoProfile(st, t, name)
				if err != nil {
					return err
				}
				profiles = []iso.Profile{p}
			} else {
				var lerr error
				if profiles, lerr = st.List(); lerr != nil {
					fmt.Fprintf(stdio.Err, "cảnh báo: bỏ qua profile hỏng: %v\n", lerr)
				}
			}
			var outs []isoSyncOut
			failed, drift := 0, false
			for _, p := range profiles {
				rep, err := app.IsoSync(cmd.Context(), p, app.IsoSyncOptions{Check: check})
				o := isoSyncOut{SyncReport: rep}
				if err != nil {
					o.Error = err.Error()
					failed++
				}
				drift = drift || rep.Drift()
				outs = append(outs, o)
			}
			if asJSON {
				if outs == nil {
					outs = []isoSyncOut{}
				}
				if err := writeJSON(stdio.Out, outs); err != nil {
					return err
				}
			} else {
				if len(outs) == 0 {
					fmt.Fprintln(stdio.Out, "Chưa có profile (bint iso create <claude|codex> --name N).")
				}
				for _, o := range outs {
					if o.Error != "" {
						fmt.Fprintf(stdio.Out, "%s: LỖI %s\n", o.Profile, o.Error)
						continue
					}
					printIsoSyncReport(stdio.Out, o.SyncReport)
				}
			}
			if failed > 0 {
				return withCode(1, fmt.Errorf("%d profile không đồng bộ được", failed))
			}
			if check && drift {
				return quietExit(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "chỉ kiểm: không ghi gì; thoát 1 khi lệch hoặc xung đột")
	c.Flags().BoolVar(&all, "all", false, "mọi profile (mặc định)")
	c.Flags().StringVar(&only, "profile", "", "chỉ một profile, dạng <tool>/<name>")
	c.Flags().BoolVar(&asJSON, "json", false, "in dạng JSON")
	return c
}

// warnIsoSyncDrift là kiểm tĩnh của run: so cấu hình profile với bản thật (không ghi, không chạy harness) và chỉ
// cảnh báo; mã thoát của run không đổi.
func warnIsoSyncDrift(ctx context.Context, stdio IO, p iso.Profile) {
	rep, err := app.IsoSync(ctx, p, app.IsoSyncOptions{Check: true})
	if err != nil {
		fmt.Fprintf(stdio.Err, "bint iso: không kiểm được đồng bộ cấu hình của %s: %v\n", p.ID(), err)
		return
	}
	n := 0
	for _, it := range rep.Items {
		if it.Action.Drift() {
			n++
			fmt.Fprintf(stdio.Err, "bint iso: %s lệch bản thật: %s\n", p.ID(), isoSyncLabel(rep, it))
		}
	}
	if n > 0 {
		fmt.Fprintf(stdio.Err, "bint iso: chạy `bint iso sync --profile %s` để đồng bộ (không đè khoá đã sửa tay)\n", p.ID())
	}
}

// printIsoFinalize chạy các bước hoàn thiện của profile mới (liên kết chia sẻ, bản cài, đồng bộ cấu hình) và in kết
// quả: dùng chung cho create và import. Lỗi từng bước chỉ là cảnh báo.
func printIsoFinalize(ctx context.Context, stdio IO, p iso.Profile) {
	printIsoFinalizeResult(stdio, p, app.IsoFinalize(ctx, p, os.Environ()))
}

func printIsoFinalizeResult(stdio IO, p iso.Profile, f app.IsoFinalizeResult) {
	if f.SharesErr != nil {
		fmt.Fprintf(stdio.Err, "cảnh báo: liên kết chia sẻ cache: %v\n", f.SharesErr)
	}
	if f.InstallErr != nil {
		fmt.Fprintf(stdio.Err, "cảnh báo: bản cài harness: %v\n", f.InstallErr)
	}
	for _, w := range f.Issues {
		fmt.Fprintf(stdio.Err, "cảnh báo: %s\n", w)
	}
	for _, r := range f.Links {
		if r.State.Conflict() {
			fmt.Fprintf(stdio.Err, "cảnh báo: %s giữ nguyên (%s), không liên kết tới %s\n", r.Link, r.State, r.Target)
		}
	}
	// Đồng bộ cấu hình từ máy thật (skill, MCP, trust, hook): lỗi hay xung đột chỉ là cảnh báo, profile vẫn dùng được.
	if f.SyncErr != nil {
		fmt.Fprintf(stdio.Err, "cảnh báo: đồng bộ cấu hình: %v (chạy lại: bint iso sync --profile %s)\n", f.SyncErr, p.ID())
		return
	}
	printIsoSyncReport(stdio.Out, f.Sync)
}
