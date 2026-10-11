package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// Sleep 上限设计：SDK 自带 sleep 允许 1-3600 秒，一次调用就能把单条意图的
// 20 分钟墙钟预算睡掉大半（实战中出现过反复 Start-Sleep 420s 轮询爆破结果、
// 界面看似卡死的案例）。但 sleep 又是 SDK「后台任务 + TaskOutput 收割」模式的
// 必要等待原语，不能删。取 300 秒：足够等过一个常见限速窗口（60s）或让后台
// 扫描跑出首批结果，又不至于一次睡掉整轮预算；更长的等待应拆成多次
// 「sleep + TaskOutput 看进度」的短循环，每轮都能被拦截/暂停/超时机制触及。
const sleepCapSeconds = 300

// cappedSleepTool 替换 SDK 默认 sleep（上限 3600s）的本平台版本：单次上限
// sleepCapSeconds，描述里明确正确用法（配合后台任务短轮询，不是长等待）。
func cappedSleepTool() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: "sleep",
		Description: fmt.Sprintf("Waits for N seconds (max %d), then returns. Use to let a backgrounded command make progress before reading its partial output with TaskOutput. For longer waits, poll in short sleep+TaskOutput cycles instead of one long sleep. Interrupted immediately if the run is paused or stopped.", sleepCapSeconds),
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"seconds": map[string]any{"type": "integer", "description": fmt.Sprintf("Seconds to wait, 1-%d.", sleepCapSeconds)},
			},
			"required": []string{"seconds"},
		},
		ReadOnly:    func(json.RawMessage) bool { return true },
		Concurrent:  func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision { return permission.Allowed() },
		Run:         runCappedSleep,
	})
}

func runCappedSleep(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
	var a struct {
		Seconds int `json:"seconds"`
	}
	_ = json.Unmarshal(in, &a)
	if a.Seconds < 1 {
		a.Seconds = 1
	}
	capped := false
	if a.Seconds > sleepCapSeconds {
		a.Seconds = sleepCapSeconds
		capped = true
	}
	select {
	case <-time.After(time.Duration(a.Seconds) * time.Second):
		if capped {
			return actool.Text(fmt.Sprintf("slept %ds (请求时长超上限已截断；需要更久请用多次 sleep+TaskOutput 短轮询)", a.Seconds)), nil
		}
		return actool.Text(fmt.Sprintf("slept %ds", a.Seconds)), nil
	case <-ctx.Done():
		return actool.Text("sleep interrupted"), nil
	}
}

// withCappedSleep 把工具列表里的 SDK 默认 sleep 换成本平台限顶版；列表中没有
// sleep 时原样返回。worker/planner/mainagent/chat 四处工具集统一走这里。
func withCappedSleep(tools []actool.CoreTool) []actool.CoreTool {
	for i, t := range tools {
		if t.Name() == "sleep" {
			tools[i] = cappedSleepTool()
			return tools
		}
	}
	return tools
}
