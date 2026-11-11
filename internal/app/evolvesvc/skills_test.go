package evolvesvc

import (
	"context"
	"testing"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/testutil"
)

func TestSkillOptionsValidateAndDefaults(t *testing.T) {
	cfg := config.Default()
	if err := SkillCheckDefaults(cfg).Validate(); !app.IsUsage(err) {
		t.Fatalf("thiếu đích phải là UsageError: %v", err)
	}
	if err := (SkillCheckOptions{Target: "  "}).Validate(); !app.IsUsage(err) {
		t.Fatalf("đích trắng: %v", err)
	}
	if err := (SkillCheckOptions{Target: "ten-skill"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := SkillRegressDefaults(cfg).Validate(); err != nil {
		t.Fatal(err)
	}
}

// Không có skill tiến hoá: regress trả danh sách rỗng (không nil) kèm mô hình đã cho; check skill lạ là lỗi.
func TestSkillRegressEmptyAndCheckUnknown(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if _, err := app.Init(context.Background(), app.InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	e, err := OpenNoIngest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	r, err := SkillRegress(context.Background(), e, SkillRegressOptions{Model: "m-test"})
	if err != nil || r.Model != "m-test" || r.Statuses == nil || len(r.Statuses) != 0 {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := SkillCheck(context.Background(), e, SkillCheckOptions{Target: "khong-co"}); err == nil {
		t.Fatal("skill lạ phải lỗi")
	}
	if _, err := SkillCheck(context.Background(), e, SkillCheckOptions{Target: "ep_khong_co"}); err == nil {
		t.Fatal("đề xuất lạ phải lỗi")
	}
}
