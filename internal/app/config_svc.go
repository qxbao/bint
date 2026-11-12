package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/skills"
)

// Dịch vụ cấu hình cho màn Hệ thống › Cấu hình và Ngôn ngữ (docs/tui-plan.md mục 5.7, 7.5): xem cấu hình
// hiệu lực theo nhóm khoá cấp một, sửa một lớp qua bản sao tạm (kiểm bằng Validate() trên cấu hình đã ghép
// lớp trước khi ghi) và ghi `ui.language`. Không có lệnh CLI tương ứng (CLI sửa tệp bằng tay hoặc `init --update`).

// LinkedWorktreeError: sửa cấu hình ở worktree phụ bị từ chối; cấu hình nằm ở worktree chính (mục 7.5).
type LinkedWorktreeError struct{ Main string }

func (e LinkedWorktreeError) Error() string {
	return fmt.Sprintf("đây là worktree phụ: sửa cấu hình ở worktree chính (cd %s && bint)", e.Main)
}

// ErrConfigChanged: tệp cấu hình đã đổi kể từ lúc sao ra bản tạm; không ghi để khỏi đè thay đổi của người khác.
var ErrConfigChanged = errors.New("tệp cấu hình đã bị đổi kể từ lúc mở để sửa; không ghi")

// ConfigGroup là một nhóm khoá cấp một của cấu hình hiệu lực (YAML của chính nhóm đó).
type ConfigGroup struct {
	Key  string
	YAML string
}

// ConfigSource là một lớp nguồn của cấu hình.
type ConfigSource struct {
	// Name: global | project | strategy | local.
	Name string
	Path string
	// Present: tệp tồn tại và đã được ghép. Editable: sửa được từ TUI (project, local).
	Present  bool
	Editable bool
}

// ConfigViewData là cấu hình hiệu lực kèm các lớp nguồn.
type ConfigViewData struct {
	Version   int
	Groups    []ConfigGroup
	Sources   []ConfigSource
	Effective config.Config
	// Root là thư mục chứa .bint/ đang dùng. LinkedWorktree: đang ở worktree phụ; Main là worktree chính.
	Root            string
	LinkedWorktree  bool
	Main            string
	StrategyProblem string
}

// ConfigView trả cấu hình hiệu lực (đã ghép lớp, đã Validate) theo từng khoá cấp một.
func ConfigView(ctx context.Context, dir string) (ConfigViewData, error) {
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return ConfigViewData{}, err
	}
	if ws.Sources.Project == "" {
		return ConfigViewData{}, ErrNotInitialized
	}
	root := filepath.Dir(filepath.Dir(ws.Sources.Project))
	v := ConfigViewData{Version: ws.Config.Version, Effective: ws.Config, Root: root, StrategyProblem: ws.Sources.StrategyProblem,
		LinkedWorktree: ws.Repo.IsLinkedWorktree(), Main: ws.Repo.MainWorktree()}
	rv, rt := reflect.ValueOf(ws.Config), reflect.TypeOf(ws.Config)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		key, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if !f.IsExported() || key == "" || key == "-" || key == "version" {
			continue
		}
		b, err := yaml.Marshal(rv.Field(i).Interface())
		if err != nil {
			return v, fmt.Errorf("nhóm %s: %w", key, err)
		}
		v.Groups = append(v.Groups, ConfigGroup{Key: key, YAML: string(b)})
	}
	src := ws.Sources
	v.Sources = []ConfigSource{
		{Name: "global", Path: config.GlobalPath(), Present: src.Global != ""},
		{Name: "project", Path: config.ProjectPath(root), Present: src.Project != "", Editable: true},
		{Name: "strategy", Path: config.StrategyPath(root), Present: src.Strategy != ""},
		{Name: "local", Path: config.LocalPath(root), Present: src.Local != "", Editable: true},
	}
	return v, nil
}

// configCtx mở workspace cho thao tác sửa cấu hình: từ chối ở worktree phụ.
func configCtx(ctx context.Context, dir string) (*Workspace, string, error) {
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return nil, "", err
	}
	if ws.Sources.Project == "" {
		return nil, "", ErrNotInitialized
	}
	if ws.Repo.IsLinkedWorktree() {
		return nil, "", LinkedWorktreeError{Main: ws.Repo.MainWorktree()}
	}
	return ws, filepath.Dir(filepath.Dir(ws.Sources.Project)), nil
}

func hashBytes(b []byte) string { return fsafe.Hash(string(b)) }

const newLocalSeed = "# .bint/local.yml: cấu hình riêng của máy này (không commit), đè lên .bint/config.yml.\n" +
	"# Chỉ ghi các khoá cần đổi, ví dụ:\n# ui:\n#   language: vi\n"

// ConfigEdit là một phiên sửa một lớp cấu hình qua bản sao tạm.
type ConfigEdit struct {
	Layer config.Layer
	// Path là tệp thật; TempPath là bản sao tạm để mở trong $EDITOR (người gọi xoá sau khi xong).
	Path, TempPath string
	// Original là nội dung tệp thật lúc sao (nil nếu chưa có); Seed là nội dung ban đầu của bản tạm
	// (khác Original khi tệp local.yml chưa có: bản tạm có đoạn chú thích hướng dẫn).
	Original, Seed []byte
	Existed        bool
	// BaseHash là băm của Original: SaveConfigEdit từ chối ghi nếu tệp thật không còn khớp.
	BaseHash string
}

// PrepareConfigEdit sao lớp cấu hình ra tệp tạm để sửa. Từ chối ở worktree phụ (LinkedWorktreeError).
func PrepareConfigEdit(ctx context.Context, dir string, layer config.Layer) (ConfigEdit, error) {
	if !layer.Valid() {
		return ConfigEdit{}, Usagef("lớp cấu hình %q không sửa được (chỉ project hoặc local)", string(layer))
	}
	_, root, err := configCtx(ctx, dir)
	if err != nil {
		return ConfigEdit{}, err
	}
	e := ConfigEdit{Layer: layer, Path: config.LayerPath(root, layer)}
	data, err := os.ReadFile(e.Path)
	switch {
	case err == nil:
		e.Existed, e.Original = true, data
		e.Seed = data
	case errors.Is(err, os.ErrNotExist):
		e.Seed = nil
		if layer == config.LayerLocal {
			e.Seed = []byte(newLocalSeed)
		}
	default:
		return ConfigEdit{}, err
	}
	e.BaseHash = hashBytes(e.Original)
	f, err := os.CreateTemp("", "bint-config-*.yml")
	if err != nil {
		return ConfigEdit{}, err
	}
	defer f.Close()
	if _, err := f.Write(e.Seed); err != nil {
		os.Remove(f.Name())
		return ConfigEdit{}, err
	}
	e.TempPath = f.Name()
	return e, nil
}

// ConfigRebase là kết quả đặt lại mốc của phiên sửa.
type ConfigRebase struct {
	// Merged: bản tạm đã được ghi lại bằng nội dung gộp (thay đổi của người dùng đặt trên tệp gốc mới).
	Merged bool
	// Conflicts là số khối xung đột trong bản tạm (có dấu ConflictMine/ConflictSep/ConflictTheirs).
	Conflicts int
}

// RebaseConfigEdit đặt lại mốc của phiên sửa theo tệp thật HIỆN TẠI sau khi ghi bị từ chối vì tệp gốc đã đổi
// (ErrConfigChanged). Thay đổi của người dùng (bản tạm so với Seed) được gộp ba chiều theo dòng lên tệp gốc mới
// (Merge3: mốc = Seed, bản của bạn = bản tạm, bản mới = tệp thật) và ghi lại vào bản tạm, nên ghi tiếp không hoàn
// tác thay đổi của người khác. Không xung đột thì bản tạm là nội dung đã gộp; có xung đột thì bản tạm mang dấu
// xung đột và CheckConfigEdit từ chối nó đến khi người dùng gỡ hết dấu. Seed/Original/BaseHash theo tệp mới nên
// diff xác nhận chỉ còn thay đổi của người dùng. Tệp gốc đã bị xoá thì giữ nguyên bản của người dùng làm nội dung
// tệp mới. Mọi lỗi (không đọc được bản tạm hoặc tệp gốc) trả về với bản tạm còn nguyên.
func RebaseConfigEdit(ctx context.Context, dir string, e ConfigEdit) (ConfigEdit, ConfigRebase, error) {
	var res ConfigRebase
	if _, _, err := configCtx(ctx, dir); err != nil {
		return e, res, err
	}
	cur, err := os.ReadFile(e.Path)
	existed := true
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		cur, existed = nil, false
	default:
		return e, res, err
	}
	ours, err := os.ReadFile(e.TempPath)
	if err != nil {
		return e, res, err
	}
	seed := []byte(nil)
	if !existed && e.Layer == config.LayerLocal {
		seed = []byte(newLocalSeed) // tệp mới: bản tạm khởi đầu bằng đoạn hướng dẫn như PrepareConfigEdit
	} else if existed {
		seed = cur
	}
	merged := string(ours)
	if existed {
		var n int
		merged, n = Merge3(string(e.Seed), string(ours), string(cur))
		res.Conflicts = n
	}
	if merged != string(ours) {
		if err := fsafe.WriteAtomic(e.TempPath, []byte(merged), 0o600); err != nil {
			return e, res, err
		}
		res.Merged = true
	}
	e.Existed, e.Original, e.Seed = existed, cur, seed
	e.BaseHash = hashBytes(e.Original)
	return e, res, nil
}

// ConfigCheck là kết quả kiểm tra bản đã sửa.
type ConfigCheck struct {
	// Changed: nội dung khác bản tạm ban đầu. Valid: cấu hình ghép lớp với nội dung mới qua Validate().
	Changed bool
	Valid   bool
	Err     string
	// Diff: thay đổi so với tệp thật (rỗng nếu không đổi).
	Diff string
}

// CheckConfigEdit kiểm bản đã sửa: không ghi gì. Lỗi được trả trong ConfigCheck.Err (không phải error) để
// màn hình cho người sửa lại.
func CheckConfigEdit(ctx context.Context, dir string, e ConfigEdit, edited []byte) (ConfigCheck, error) {
	_, root, err := configCtx(ctx, dir)
	if err != nil {
		return ConfigCheck{}, err
	}
	c := ConfigCheck{Changed: !bytes.Equal(edited, e.Seed)}
	if !c.Changed {
		c.Valid = true
		return c, nil
	}
	if HasConflictMarkers(edited) {
		c.Err = "còn dấu xung đột (" + ConflictMine + " … " + ConflictTheirs + "): gộp tay rồi xoá các dòng dấu"
		c.Diff = skills.Diff(e.Path+" (hiện tại)", e.Path+" (sau khi sửa)", string(e.Original), string(edited))
		return c, nil
	}
	if _, err := config.ValidateWith(root, e.Layer, edited); err != nil {
		c.Err = err.Error()
	} else {
		c.Valid = true
	}
	c.Diff = skills.Diff(e.Path+" (hiện tại)", e.Path+" (sau khi sửa)", string(e.Original), string(edited))
	return c, nil
}

// ConfigSaveResult là kết quả ghi cấu hình.
type ConfigSaveResult struct {
	Path string
	// Backup là bản sao lưu của tệp cũ trong thư mục trạng thái (rỗng nếu tệp chưa có).
	Backup string
}

// SaveConfigEdit ghi bản đã sửa vào lớp cấu hình: kiểm lại bằng Validate() trên cấu hình đã ghép lớp, từ chối
// nếu tệp thật đã đổi kể từ lúc sao (ErrConfigChanged), sao lưu tệp cũ vào thư mục trạng thái rồi ghi nguyên tử.
func SaveConfigEdit(ctx context.Context, dir string, e ConfigEdit, edited []byte) (ConfigSaveResult, error) {
	ws, root, err := configCtx(ctx, dir)
	if err != nil {
		return ConfigSaveResult{}, err
	}
	if _, err := config.ValidateWith(root, e.Layer, edited); err != nil {
		return ConfigSaveResult{}, err
	}
	return writeConfigLayer(ws, root, e.Layer, e.BaseHash, edited)
}

// writeConfigLayer ghi nội dung data vào lớp sau khi so băm tệp hiện tại với baseHash.
func writeConfigLayer(ws *Workspace, root string, layer config.Layer, baseHash string, data []byte) (ConfigSaveResult, error) {
	path := config.LayerPath(root, layer)
	res := ConfigSaveResult{Path: path}
	cur, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	if hashBytes(cur) != baseHash {
		return res, ErrConfigChanged
	}
	if err == nil && ws.Paths.Backups != "" {
		rel := filepath.ToSlash(filepath.Join(config.DirName, filepath.Base(path)))
		if res.Backup, err = fsafe.Backup(path, ws.Paths.Backups, rel, time.Now()); err != nil {
			return res, err
		}
	}
	return res, fsafe.WriteAtomic(path, data, 0o644)
}

// UILanguageOptions điều khiển việc ghi `ui.language` (Hệ thống › Ngôn ngữ).
type UILanguageOptions struct {
	Language string `flag:"language" form:"required,choices=auto|vi|en"`
	// Layer: local (.bint/local.yml, chỉ máy này) hoặc project (.bint/config.yml, cả nhóm).
	Layer string `flag:"layer" form:"choices=local|project"`
}

// UILanguageDefaults: giá trị hiện hành, ghi vào lớp cục bộ.
func UILanguageDefaults(cfg config.Config) UILanguageOptions {
	return UILanguageOptions{Language: cfg.UI.Language, Layer: string(config.LayerLocal)}
}

func (o UILanguageOptions) Validate() error {
	ok := false
	for _, l := range config.UILanguages {
		ok = ok || l == o.Language
	}
	if !ok {
		return Usagef("ui.language %q: chỉ nhận %s", o.Language, strings.Join(config.UILanguages, " | "))
	}
	if o.Layer != "" && !config.Layer(o.Layer).Valid() {
		return Usagef("nơi ghi %q: chỉ nhận local hoặc project", o.Layer)
	}
	return nil
}

// UILanguageResult là kết quả ghi ngôn ngữ.
type UILanguageResult struct {
	Path     string
	Language string
	Layer    config.Layer
	// Changed: tệp đã được ghi (false = giá trị đã đúng sẵn ở lớp đó).
	Changed bool
	Backup  string
	// Effective là ui.language HIỆU LỰC sau khi ghi (đã ghép lớp toàn cục → dự án → strategy → cục bộ). ShadowedBy là lớp cao hơn
	// đang che giá trị vừa ghi (Effective khác Language), rỗng nếu không bị che. (--lang và BINT_LANG nằm ngoài cấu hình: TUI tự xét.)
	Effective  string
	ShadowedBy config.Layer
}

// SetUILanguage ghi ui.language vào lớp cấu hình, chỉ sửa đúng dòng cần sửa (giữ chú thích và các dòng khác),
// chèn khối ui khi thiếu. Kiểm bằng Validate() trên cấu hình đã ghép lớp trước khi ghi; từ chối ở worktree phụ.
func SetUILanguage(ctx context.Context, dir string, o UILanguageOptions) (UILanguageResult, error) {
	if err := o.Validate(); err != nil {
		return UILanguageResult{}, err
	}
	layer := config.Layer(o.Layer)
	if layer == "" {
		layer = config.LayerLocal
	}
	ws, root, err := configCtx(ctx, dir)
	if err != nil {
		return UILanguageResult{}, err
	}
	path := config.LayerPath(root, layer)
	res := UILanguageResult{Path: path, Language: o.Language, Layer: layer}
	cur, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	next, changed, err := config.SetUILanguage(cur, o.Language)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	merged, err := config.ValidateWith(root, layer, next)
	if err != nil && !(!changed && cur != nil) {
		return res, err
	}
	if err == nil {
		res.Effective = merged.UI.Language
		if layer == config.LayerProject && res.Effective != o.Language {
			res.ShadowedBy = config.LayerLocal // chỉ lớp cục bộ đứng trên lớp dự án
		}
	}
	if !changed && cur != nil {
		return res, nil
	}
	w, err := writeConfigLayer(ws, root, layer, hashBytes(cur), next)
	res.Changed, res.Backup = err == nil, w.Backup
	return res, err
}
