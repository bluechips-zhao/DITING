package report

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
)

func reportCell(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|").Replace(value)
	return html.EscapeString(value)
}

// CompanyMarkdown renders stored records only; neither template invokes an LLM.
func CompanyMarkdown(snapshot *db.CompanyReportSnapshot, template string, redacted bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 企业安全报告 — %s\n\n", reportCell(snapshot.CompanyName))
	fmt.Fprintf(&b, "- 企业 ID：%d\n- 数据快照：%s\n- 模板：%s\n\n", snapshot.CompanyID, snapshot.GeneratedAt.Format(time.RFC3339), template)
	if redacted {
		b.WriteString("本报告为脱敏副本：隐藏已识别的凭据，不附原始流量报文。自定义、无标记的敏感内容仍需人工核对。\n\n")
	}
	b.WriteString("## 汇总范围\n\n按当前资产归属汇总，只纳入全部资产锚点均属于本企业的发现；不自动纳入未关联资产的发现或继承任务的其他记录。\n\n")
	fmt.Fprintf(&b, "跨归属或缺失资产锚点、且与本企业资产存在关联的发现：%d 条，未并入明细。资产数量和任务状态不代表已完成安全覆盖。\n\n", snapshot.MixedExcluded)
	types := make([]string, 0, len(snapshot.AssetCounts))
	for typ := range snapshot.AssetCounts {
		types = append(types, typ)
	}
	sort.Strings(types)
	b.WriteString("## 当前资产\n\n| 类型 | 数量 |\n| --- | --- |\n")
	for _, typ := range types {
		fmt.Fprintf(&b, "| %s | %d |\n", reportCell(typ), snapshot.AssetCounts[typ])
	}
	findings := snapshot.Findings
	if redacted {
		findings = RedactedFindings(findings)
	}
	statuses := map[string]int{}
	for _, f := range findings {
		statuses[nz(f.Status, "pending")]++
	}
	b.WriteString("\n## 发现状态\n\n| 复核状态 | 数量 |\n| --- | --- |\n")
	keys := make([]string, 0, len(statuses))
	for key := range statuses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "| %s | %d |\n", reportCell(key), statuses[key])
	}
	b.WriteString("\n待复核、误报或已接受风险不等于已确认漏洞；没有记录也不证明不存在漏洞。\n\n## 当前相关任务\n\n| 来源任务 ID | 状态 |\n| --- | --- |\n")
	for _, task := range snapshot.Tasks {
		fmt.Fprintf(&b, "| %d | %s |\n", task.ID, reportCell(task.Status))
	}
	if template == "executive" {
		b.WriteString("\n## 发现概览\n\n| 发现 ID | 名称 | 等级 | 复核状态 | 证据版本 | 报告版本 |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %d | %d |\n", f.ID, reportCell(findingTitle(f)), reportCell(f.Severity), reportCell(f.Status), f.EvidenceVersion, f.ReportEvidenceVersion)
		}
	} else {
		detail := FindingsMarkdown(findings, snapshot.GeneratedAt)
		if start := strings.Index(detail, "## 漏洞明细"); start >= 0 {
			b.WriteString("\n" + detail[start:])
		} else {
			b.WriteString("\n暂无符合汇总范围的发现记录。\n")
		}
	}
	b.WriteString("\n## 最新复测记录\n\n| 发现 ID | 复测 ID | 记录状态 | 结论 | 开始时证据版本 | 记录时间 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, r := range snapshot.Retests {
		version := "未记录/不可比较"
		if r.BaselineVersion != nil {
			version = fmt.Sprint(*r.BaselineVersion)
		}
		verdict := "尚无完成结论"
		if r.Status == "completed" {
			switch r.Verdict {
			case "fixed":
				verdict = "已修复"
			case "reproduced":
				verdict = "仍可复现"
			default:
				verdict = "无法确认"
			}
		}
		fmt.Fprintf(&b, "| %d | %d | %s | %s | %s | %s |\n", r.FindingID, r.ID, reportCell(r.Status), verdict, version, r.CreatedAt.Format(time.RFC3339))
	}
	b.WriteString("\n复测是针对开始时快照的独立记录，不自动证明当前版本已验证；具体证据通过发现和复测 ID 追溯。\n")
	if redacted {
		return RedactText(b.String())
	}
	return b.String()
}
