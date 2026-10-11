// Package report renders a deliverable Markdown report from the exploration
// graph: confirmed findings (with severity/evidence) (docs §15 P5).
package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
)

// Input bundles what the report needs.
type Input struct {
	Title        string
	Goal         string
	GeneratedAt  time.Time
	AssetCounts  map[string]int
	Findings     []*db.Node
	Records      []*db.DBFinding
	Goals        []*db.Node
	IntentStates map[string]int
}

type findingView struct {
	VulnClass string
	Name      string
	Severity  string
	Summary   string
	PoC       string
}

func parseFinding(n *db.Node) findingView {
	var p struct {
		VulnClass string `json:"vulnclass"`
		Name      string `json:"name"`
		Severity  string `json:"severity"`
		Summary   string `json:"summary"`
		Evidence  struct {
			PoC string `json:"poc"`
		} `json:"evidence"`
	}
	_ = json.Unmarshal(n.Payload, &p)
	return findingView{p.VulnClass, p.Name, p.Severity, p.Summary, p.Evidence.PoC}
}

var sevRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "": 4}

// Markdown renders the report.
func Markdown(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 渗透测试报告 — %s\n\n", nz(in.Title, "未命名任务"))
	fmt.Fprintf(&b, "- **任务目标**：%s\n", nz(in.Goal, "（未指定）"))
	fmt.Fprintf(&b, "- **生成时间**：%s\n\n", in.GeneratedAt.Format("2006-01-02 15:04:05"))

	// summary
	fmt.Fprintf(&b, "## 摘要\n\n")
	if in.Records != nil {
		statuses := map[string]int{}
		for _, f := range in.Records {
			statuses[nz(f.Status, "pending")]++
		}
		fmt.Fprintf(&b, "- 发现总数：**%d** 个\n", len(in.Records))
		for _, status := range []string{"confirmed", "pending", "in_progress", "resolved", "fixed", "false_positive", "ignored", "duplicate", "risk_accepted"} {
			if statuses[status] > 0 {
				fmt.Fprintf(&b, "  - %s：%d\n", status, statuses[status])
			}
		}
	} else {
		fmt.Fprintf(&b, "- 探索图发现：**%d** 个（未包含人工复核状态）\n", len(in.Findings))
	}
	fmt.Fprintf(&b, "- 本任务关联资产：")
	var types []string
	for t := range in.AssetCounts {
		types = append(types, t)
	}
	sort.Strings(types)
	for i, t := range types {
		if i > 0 {
			b.WriteString("、")
		}
		fmt.Fprintf(&b, "%s %d", t, in.AssetCounts[t])
	}
	b.WriteString("\n\n")
	if in.Goals != nil {
		b.WriteString("## 目标与未完成工作\n\n")
		for _, g := range in.Goals {
			var p struct {
				Text  string        `json:"text"`
				Proof *db.GoalProof `json:"proof"`
			}
			_ = json.Unmarshal(g.Payload, &p)
			fmt.Fprintf(&b, "- [%s] %s\n", g.State, p.Text)
			if p.Proof != nil {
				fmt.Fprintf(&b, "  - 证据节点 #%s：%s\n", p.Proof.EvidenceID, p.Proof.Reason)
			}
		}
		states := make([]string, 0, len(in.IntentStates))
		for state := range in.IntentStates {
			states = append(states, state)
		}
		sort.Strings(states)
		for _, state := range states {
			fmt.Fprintf(&b, "- 意图 %s：%d\n", state, in.IntentStates[state])
		}
		b.WriteString("\n未完成、阻塞和耗尽方向不代表已验证安全。覆盖资产关联也不代表每项已完成测试；本报告不自动混入继承任务记录。\n\n")
	}
	if in.Records != nil {
		b.WriteString("以下保留全部发现及其复核状态；待复核与误报不计入确认数。资产统计仅包含本任务关联资产。\n\n")
		detail := FindingsMarkdown(in.Records, in.GeneratedAt)
		if at := strings.Index(detail, "## 漏洞明细"); at >= 0 {
			b.WriteString(detail[at:])
		} else {
			b.WriteString("_本任务暂无漏洞记录。_\n")
		}
		return b.String()
	}

	// findings
	fmt.Fprintf(&b, "## 发现\n\n")
	if len(in.Findings) == 0 {
		b.WriteString("_本次未确认漏洞。_\n\n")
	} else {
		fs := make([]findingView, 0, len(in.Findings))
		for _, n := range in.Findings {
			fs = append(fs, parseFinding(n))
		}
		sort.SliceStable(fs, func(i, j int) bool { return sevRank[fs[i].Severity] < sevRank[fs[j].Severity] })
		for i, f := range fs {
			fmt.Fprintf(&b, "### %d. [%s] %s\n\n", i+1, strings.ToUpper(nz(f.Severity, "info")), nz(f.Name, nz(f.VulnClass, "未分类")))
			fmt.Fprintf(&b, "%s\n\n", nz(f.Summary, ""))
			if f.PoC != "" {
				fmt.Fprintf(&b, "**PoC / 证据：**\n\n```\n%s\n```\n\n", f.PoC)
			}
		}
	}

	return b.String()
}

func nz(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
