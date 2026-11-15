package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

func TestStepCostsTable(t *testing.T) {
	flowSteps := []string{"b-req", "b-feat", "b-review"}
	rows := []TaskRunRow{
		{RunID: "D", Workflow: "b-do", Status: "completed", CostUSD: f64(9)}, // điều phối: không cộng
		{RunID: "1", Workflow: "b-req", Parent: "D", Status: "completed", CostUSD: f64(0.5), InputTokens: i64(100), OutputTokens: i64(10), DurationMS: i64(2000)},
		{RunID: "2", Workflow: "b-feat", Parent: "D", Status: "completed", CostUSD: f64(1), InputTokens: i64(1000), OutputTokens: i64(50), DurationMS: i64(3000)},
		{RunID: "3", Workflow: "b-feat", Parent: "D", Status: "completed", CostUSD: f64(0.25), InputTokens: i64(10), OutputTokens: i64(5)}, // vòng sửa
		{RunID: "4", Workflow: "b-feat", Parent: "D", Status: "failed"},                                                                    // thiếu chi phí
		{RunID: "5", Workflow: "lạ", Parent: "D", Status: "running"},                                                                       // bước lạ, đang chạy
	}
	steps, total, partial := StepCosts(rows, flowSteps)
	if len(steps) != 4 || steps[0].Step != "b-req" || steps[1].Step != "b-feat" || steps[2].Step != "b-review" || steps[3].Step != "lạ" {
		t.Fatalf("thứ tự: %+v", steps)
	}
	if steps[2].Count != 0 || steps[2].CostUSD != nil || steps[2].LastStatus != "" || !steps[2].InFlow || steps[3].InFlow {
		t.Fatalf("bước chưa chạy / lạ: %+v %+v", steps[2], steps[3])
	}
	feat := steps[1]
	if feat.Count != 3 || feat.CostUSD == nil || *feat.CostUSD != 1.25 || !feat.CostMissing || feat.InputTokens != 1010 || feat.OutputTokens != 55 || feat.LastStatus != "failed" {
		t.Fatalf("b-feat: %+v", feat)
	}
	if steps[0].Duration.Seconds() != 2 || steps[3].CostMissing || steps[3].CostUSD != nil {
		t.Fatalf("thời lượng / lượt running: %+v %+v", steps[0], steps[3])
	}
	if total == nil || *total != 1.75 || !partial {
		t.Fatalf("tổng: %v %v", total, partial)
	}
	_, total, partial = StepCosts(nil, flowSteps)
	if total != nil || partial {
		t.Fatalf("rỗng: %v %v", total, partial)
	}
}

func TestTaskRunRowJSONKeepsOldKeys(t *testing.T) {
	b, _ := json.Marshal(TaskRunRow{RunID: "r", Workflow: "w", Status: "s", DurationMS: i64(5)})
	s := string(b)
	for _, k := range []string{`"run_id"`, `"workflow"`, `"status"`, `"started_at"`, `"duration"`, `"cost_usd":null`} {
		if !strings.Contains(s, k) {
			t.Fatalf("thiếu %s: %s", k, s)
		}
	}
	for _, k := range []string{"model", "harness", "input_tokens", "output_tokens", "DurationMS"} {
		if strings.Contains(s, k) {
			t.Fatalf("không được có %s khi rỗng: %s", k, s)
		}
	}
	b, _ = json.Marshal(TaskRunRow{Model: "m", Harness: "h", InputTokens: i64(1), OutputTokens: i64(2)})
	for _, k := range []string{`"model":"m"`, `"harness":"h"`, `"input_tokens":1`, `"output_tokens":2`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("thiếu %s: %s", k, b)
		}
	}
}

func TestShowRunReportsCostAndTokens(t *testing.T) {
	root, _ := runsRepo(t)
	ctx := context.Background()
	rw, err := OpenRuns(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	rows, _ := ListRuns(ctx, rw, RunsListOptions{Limit: 5})
	id := rows[0].Run.RunID
	d, err := ShowRun(ctx, rw, RunsShowOptions{Ref: id})
	if err != nil || d.CostUSD != nil {
		t.Fatalf("chưa có chi phí: %v %+v", err, d.CostUSD)
	}
	if _, err := rw.Store.DB.Exec(`UPDATE runs SET cost_usd = 0.42, input_tokens = 7, output_tokens = 3 WHERE run_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	d, err = ShowRun(ctx, rw, RunsShowOptions{Ref: id})
	if err != nil || d.CostUSD == nil || *d.CostUSD != 0.42 || d.InputTokens == nil || *d.InputTokens != 7 || *d.OutputTokens != 3 {
		t.Fatalf("chi phí: %v %+v", err, d)
	}
}
