package app

import (
	"context"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/harness"
	"bint/internal/hcomp"
	syscreen "bint/internal/tui/screens/system"
)

// sampleSystemLoaders là dữ liệu mẫu xác định cho màn Hệ thống (không chạm repository thật, không gọi chương trình harness).
func sampleSystemLoaders() syscreen.Loaders {
	return syscreen.Loaders{
		Config: func(context.Context, string) (bintapp.ConfigViewData, error) { return sampleConfig(), nil },
		Harness: func(context.Context, string) (syscreen.HarnessData, error) {
			var infos []harness.Info
			for _, a := range harness.All() {
				in := harness.Describe(a, config.Default(), nil)
				in.Binary, in.Home, in.Installed, in.Version = "", "", false, ""
				if in.Name == "claude" {
					in.Installed, in.Binary = true, "/usr/local/bin/claude"
				}
				infos = append(infos, in)
			}
			return syscreen.HarnessData{Infos: infos}, nil
		},
		Runtime: func(context.Context, string) ([]bintapp.HarnessStatus, error) {
			return []bintapp.HarnessStatus{{Name: "claude",
				Hook: &bintapp.HookStatus{Level: bintapp.LevelOK, Detail: "phạm vi project", Scopes: []string{"project"}},
				MCP:  &bintapp.MCPStatus{Registered: true}}}, nil
		},
		Components: func(context.Context, string) (syscreen.ComponentsData, error) { return sampleComponents(), nil },
		Skills: func(context.Context, string) (bintapp.SkillList, error) {
			return bintapp.SkillList{Rows: []bintapp.SkillRow{
				{Name: "b-do", Version: "aaaa1111", Overlay: "-", Published: "ok", Kind: "core", Locked: "aaaa1111", Anchors: []string{"project-rules"},
					Targets: map[string]string{".claude/skills": "current"}},
				{Name: "b-feat", Version: "bbbb2222", Overlay: "1 khối", Published: "x", Kind: "core", Locked: "0000", Anchors: []string{"test-strategy"},
					Targets: map[string]string{".claude/skills": "differs"}},
				{Name: "api-guide", Version: "v2", Overlay: "-", Published: "x", Kind: "evolved", Status: "active", Anchors: []string{"api/**"},
					Targets: map[string]string{".claude/skills": "missing"}},
			}, Orphans: []string{".bint/skills/overlays/cu.md"}}, nil
		},
		MCP: func(context.Context, string, string) ([]bintapp.MCPTool, error) {
			return []bintapp.MCPTool{{Name: "context_for_task", Description: "Gói ngữ cảnh cho một tác vụ"}, {Name: "knowledge_search", Description: "Tìm tri thức theo từ khoá"},
				{Name: "cite_knowledge", Description: "Báo mục tri thức đã áp dụng"}}, nil
		},
		Doctor: func(context.Context, string) (*bintapp.DoctorReport, error) {
			return &bintapp.DoctorReport{Checks: []bintapp.Check{
				{Name: "git", Level: bintapp.LevelOK, Detail: "2.47.0"},
				{Name: "cấu hình", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"},
				{Name: "hook claude", Level: bintapp.LevelFail, Detail: "chưa cài; lượt chạy trực tiếp sẽ không được ghi nhận"},
				{Name: "tri thức", Level: bintapp.LevelInfo, Detail: "3 mục"},
			}}, nil
		},
	}
}

func sampleConfig() bintapp.ConfigViewData {
	keys := []string{"project", "state", "harnesses", "roles", "context", "knowledge", "memory", "eval", "evolution", "workflow", "limits", "security", "ui"}
	var gs []bintapp.ConfigGroup
	for _, k := range keys {
		y := "name: demo\nmode: x\n"
		if k == "ui" {
			y = "language: auto\n"
		}
		gs = append(gs, bintapp.ConfigGroup{Key: k, YAML: y})
	}
	return bintapp.ConfigViewData{Version: 1, Groups: gs, Effective: config.Default(), Root: "/work/demo",
		Sources: []bintapp.ConfigSource{
			{Name: "global", Path: "/home/u/.config/bint/config.yml"},
			{Name: "project", Path: "/work/demo/.bint/config.yml", Present: true, Editable: true},
			{Name: "strategy", Path: "/work/demo/.bint/harness/strategy.yml"},
			{Name: "local", Path: "/work/demo/.bint/local.yml", Editable: true},
		}}
}

func sampleComponents() syscreen.ComponentsData {
	mk := func(c hcomp.Component, content string) bintapp.ComponentDetail {
		return bintapp.ComponentDetail{Component: c, Row: bintapp.RowOfComponent(c), Content: content, Script: string(c.Source)}
	}
	guard := hcomp.Component{Kind: hcomp.KindGuardrail, Meta: hcomp.Meta{ID: "test-before-stop", Scope: "global", Status: hcomp.StatusVerified, Origin: hcomp.OriginHuman, Version: 1, ApprovedBy: "An"},
		On: "stop", Action: "remind", Message: "Chạy test trước khi kết thúc.", File: "guardrails/test-before-stop.yml"}
	guid := hcomp.Component{Kind: hcomp.KindGuidance, Meta: hcomp.Meta{ID: "api-style", Scope: "domain:api/**", Status: hcomp.StatusCandidate, Origin: hcomp.OriginAgent, Version: 1},
		Title: "Quy ước API", Body: "Dùng /v1.", File: "guidance/api-style.md"}
	script := hcomp.Component{Kind: hcomp.KindScript, Meta: hcomp.Meta{ID: "fmt-check", Scope: "global", Status: hcomp.StatusCandidate, Origin: hcomp.OriginAgent, Version: 1},
		Description: "Kiểm tra định dạng", Source: []byte("#!/bin/sh\ngofmt -l .\n"), File: "scripts/fmt-check.yml"}
	return syscreen.ComponentsData{
		Details:   []bintapp.ComponentDetail{mk(guard, "id: test-before-stop\non: stop\naction: remind\n"), mk(guid, "# Quy ước API\n\nDùng /v1.\n"), mk(script, "id: fmt-check\ndescription: Kiểm tra định dạng\n")},
		Problems:  []string{"guidance/hong.md: thiếu tiêu đề"},
		Templates: []string{"test-before-stop"},
	}
}

// withSampleSystem điền các hàm nạp còn nil bằng dữ liệu mẫu.
func withSampleSystem(l syscreen.Loaders) syscreen.Loaders {
	s := sampleSystemLoaders()
	if l.Config == nil {
		l.Config = s.Config
	}
	if l.Harness == nil {
		l.Harness = s.Harness
	}
	if l.Runtime == nil {
		l.Runtime = s.Runtime
	}
	if l.Components == nil {
		l.Components = s.Components
	}
	if l.Skills == nil {
		l.Skills = s.Skills
	}
	if l.MCP == nil {
		l.MCP = s.MCP
	}
	if l.Doctor == nil {
		l.Doctor = s.Doctor
	}
	return l
}
