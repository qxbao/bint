package task

import (
	"maps"
	"slices"
)

// Durable là bản chiếu bền của State: phần của phiếu đáng chia sẻ qua Git (`.bint/tasks/<ID>/state.json`).
// Không có trường riêng của máy (runner, bước dở, worktree, ảnh chụp cây), không có phê duyệt của người
// (verify/protected/setup) và không có đường dẫn log hay dấu vân tay/cây của lần kiểm chứng. Nhập giải mã vào
// Durable nên một tệp trong cây không thể đặt các trường đó vào State: lệnh kiểm chứng của phiếu nhập về vẫn
// chờ `bint task approve` trên máy này.
type Durable struct {
	Schema                  int                    `json:"schema"`
	ID                      string                 `json:"id"`
	Status                  string                 `json:"status"`
	Workflow                string                 `json:"workflow"`
	FlowSHA                 string                 `json:"flow_sha,omitempty"`
	AgentChoices            map[string]AgentChoice `json:"agent_choices,omitempty"`
	CreatedAt               string                 `json:"created_at"`
	UpdatedAt               string                 `json:"updated_at"`
	Base                    string                 `json:"base,omitempty"`
	Landed                  *DurableLanding        `json:"landed,omitempty"`
	Reason                  string                 `json:"reason,omitempty"`
	ResumeFrom              string                 `json:"resume_from,omitempty"`
	Classification          *Classification        `json:"classification,omitempty"`
	PlanningContractVersion int                    `json:"planning_contract_version,omitempty"`
	History                 []Transition           `json:"history"`
	Criteria                []CriterionCheck       `json:"criteria,omitempty"`
	Verifications           []DurableVerification  `json:"verifications,omitempty"`
	Errors                  []ErrorRecord          `json:"errors,omitempty"`
	Runs                    []string               `json:"runs,omitempty"`
	ReviewRounds            int                    `json:"review_rounds"`
	FromFindings            []string               `json:"from_findings,omitempty"`
	Parent                  string                 `json:"parent,omitempty"`
	DependsOn               []string               `json:"depends_on,omitempty"`
	Inherited               []string               `json:"inherited,omitempty"`
	NextFinding             int                    `json:"next_finding"`
	Artifacts               map[string]string      `json:"artifacts,omitempty"`
	ReviewDecided           int                    `json:"review_decided,omitempty"`
}

// DurableVerification là một lần kiểm chứng không kèm log và dấu vân tay/cây của máy.
type DurableVerification struct {
	At        string `json:"at"`
	Command   string `json:"command"`
	Confirmed bool   `json:"confirmed"`
	ExitCode  int    `json:"exit_code"`
	Passed    bool   `json:"passed"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	By        string `json:"by"`
	RunID     string `json:"run_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// DurableLanding là lần đưa về cây gốc không kèm fingerprint/tree của máy.
type DurableLanding struct {
	At          string   `json:"at"`
	By          string   `json:"by"`
	Files       []string `json:"files"`
	Conflicts   []string `json:"conflicts,omitempty"`
	Unmerged    []string `json:"unmerged,omitempty"`
	ThreeWay    bool     `json:"three_way,omitempty"`
	Kept        bool     `json:"kept,omitempty"`
	Note        string   `json:"note,omitempty"`
	Forced      bool     `json:"forced,omitempty"`
	ForceReason string   `json:"force_reason,omitempty"`
	Bypassed    []string `json:"bypassed,omitempty"`
}

// ToDurable chiếu State sang bản bền (sao chép sâu các lát để người gọi sửa thoải mái).
func ToDurable(s State) Durable {
	d := Durable{Schema: s.Schema, ID: s.ID, Status: s.Status, Workflow: s.Workflow, FlowSHA: s.FlowSHA,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Base: s.Base, Reason: s.Reason, ResumeFrom: s.ResumeFrom,
		PlanningContractVersion: s.PlanningContractVersion, ReviewRounds: s.ReviewRounds, Parent: s.Parent,
		NextFinding: s.NextFinding, ReviewDecided: s.ReviewDecided,
		History:       append([]Transition{}, s.History...),
		Criteria:      slices.Clone(s.Criteria),
		Errors:        slices.Clone(s.Errors),
		Runs:          slices.Clone(s.Runs),
		FromFindings:  slices.Clone(s.FromFindings),
		DependsOn:     slices.Clone(s.DependsOn),
		Inherited:     slices.Clone(s.Inherited),
		AgentChoices:  cloneMap(s.AgentChoices),
		Artifacts:     cloneMap(s.Artifacts),
		Verifications: nil,
	}
	if s.Classification != nil {
		c := *s.Classification
		c.IsolationTasks = slices.Clone(c.IsolationTasks)
		d.Classification = &c
	}
	for _, v := range s.Verifications {
		d.Verifications = append(d.Verifications, DurableVerification{At: v.At, Command: v.Command, Confirmed: v.Confirmed,
			ExitCode: v.ExitCode, Passed: v.Passed, TimedOut: v.TimedOut, By: v.By, RunID: v.RunID, Reason: v.Reason})
	}
	if l := s.Landed; l != nil {
		d.Landed = &DurableLanding{At: l.At, By: l.By, Files: slices.Clone(l.Files), Conflicts: slices.Clone(l.Conflicts),
			Unmerged: slices.Clone(l.Unmerged), ThreeWay: l.ThreeWay, Kept: l.Kept, Note: l.Note, Forced: l.Forced,
			ForceReason: l.ForceReason, Bypassed: slices.Clone(l.Bypassed)}
	}
	return d
}

func cloneMap[V any](m map[string]V) map[string]V {
	if m == nil {
		return nil
	}
	return maps.Clone(m)
}

// ApplyTo ghi các trường bền vào s, giữ nguyên trường riêng của máy và mọi phê duyệt. Log/fingerprint/tree của
// lần kiểm chứng (và của lần land) cục bộ được giữ khi lần đó khớp với bản bền.
func (d Durable) ApplyTo(s *State) {
	s.Schema, s.ID, s.Status, s.Workflow, s.FlowSHA = d.Schema, d.ID, d.Status, d.Workflow, d.FlowSHA
	s.CreatedAt, s.UpdatedAt, s.Base, s.Reason, s.ResumeFrom = d.CreatedAt, d.UpdatedAt, d.Base, d.Reason, d.ResumeFrom
	s.PlanningContractVersion, s.ReviewRounds, s.Parent = d.PlanningContractVersion, d.ReviewRounds, d.Parent
	s.NextFinding, s.ReviewDecided = d.NextFinding, d.ReviewDecided
	s.AgentChoices, s.Artifacts = cloneMap(d.AgentChoices), cloneMap(d.Artifacts)
	s.History, s.Criteria, s.Errors, s.Runs = append([]Transition{}, d.History...), slices.Clone(d.Criteria), slices.Clone(d.Errors), slices.Clone(d.Runs)
	s.FromFindings, s.DependsOn, s.Inherited = slices.Clone(d.FromFindings), slices.Clone(d.DependsOn), slices.Clone(d.Inherited)
	if d.Classification != nil {
		c := *d.Classification
		c.IsolationTasks = slices.Clone(c.IsolationTasks)
		s.Classification = &c
	} else {
		s.Classification = nil
	}
	old := s.Verifications
	s.Verifications = nil
	for _, v := range d.Verifications {
		nv := Verification{At: v.At, Command: v.Command, Confirmed: v.Confirmed, ExitCode: v.ExitCode, Passed: v.Passed,
			TimedOut: v.TimedOut, By: v.By, RunID: v.RunID, Reason: v.Reason}
		for _, o := range old {
			if o.At == v.At && o.Command == v.Command && o.Passed == v.Passed && o.ExitCode == v.ExitCode &&
				o.TimedOut == v.TimedOut && o.Confirmed == v.Confirmed && o.By == v.By && o.RunID == v.RunID {
				// Dấu vân tay chỉ giữ khi kết quả khớp hẳn bản cục bộ; nếu cây đổi kết quả thì không gán dấu của máy cho nó.
				nv.Log, nv.Worktree, nv.Tree = o.Log, o.Worktree, o.Tree
				break
			}
		}
		s.Verifications = append(s.Verifications, nv)
	}
	if d.Landed == nil {
		s.Landed = nil
		return
	}
	l := d.Landed
	nl := &Landing{At: l.At, By: l.By, Files: slices.Clone(l.Files), Conflicts: slices.Clone(l.Conflicts), Unmerged: slices.Clone(l.Unmerged),
		ThreeWay: l.ThreeWay, Kept: l.Kept, Note: l.Note, Forced: l.Forced, ForceReason: l.ForceReason, Bypassed: slices.Clone(l.Bypassed)}
	if s.Landed != nil && s.Landed.At == l.At {
		nl.Fingerprint, nl.Tree = s.Landed.Fingerprint, s.Landed.Tree
	}
	s.Landed = nl
}
