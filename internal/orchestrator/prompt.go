package orchestrator

import (
	"fmt"
	"strings"
)

func BuildWorkspacePrompt(title, summary string) string {
	sections := []string{
		"你是工作台前台投影会话，不是直接执行层。",
	}
	if title != "" {
		sections = append(sections, fmt.Sprintf("Mission 标题：%s", title))
	}
	if summary != "" {
		sections = append(sections, fmt.Sprintf("Mission 摘要：%s", summary))
	}
	sections = append(sections, "你的职责是向用户展示摘要、派发结果、审批镜像和只读详情。")
	return strings.Join(sections, "\n")
}

func BuildPlannerPrompt(title, summary string, tasks int) string {
	sections := []string{
		"你是 PlannerSession，只负责规划和结构化派发，不直接执行远程命令。",
	}
	if title != "" {
		sections = append(sections, fmt.Sprintf("Mission 标题：%s", title))
	}
	if summary != "" {
		sections = append(sections, fmt.Sprintf("Mission 摘要：%s", summary))
	}
	if tasks > 0 {
		sections = append(sections, fmt.Sprintf("当前待派发任务数：%d", tasks))
	}
	sections = append(sections, "如果已经形成结构化任务，请调用 orchestrator_dispatch_tasks。")
	return strings.Join(sections, "\n")
}

func BuildWorkerPrompt(hostID, title, instruction string, constraints []string, cwd string) string {
	sections := []string{
		fmt.Sprintf("你是绑定到 host=%s 的 WorkerSession。", hostID),
		"你不是直接对用户回复；你的结果会由调度器回投给 WorkspaceSession。",
		"只在当前主机范围内行动。",
	}
	if cwd != "" {
		sections = append(sections, fmt.Sprintf("默认工作区：%s", cwd))
	}
	if title != "" {
		sections = append(sections, fmt.Sprintf("任务目标：%s", title))
	}
	if instruction != "" {
		sections = append(sections, fmt.Sprintf("任务说明：%s", instruction))
	}
	if len(constraints) > 0 {
		sections = append(sections, "约束：")
		for _, constraint := range constraints {
			if strings.TrimSpace(constraint) == "" {
				continue
			}
			sections = append(sections, "- "+strings.TrimSpace(constraint))
		}
	}
	sections = append(sections, "输出要求：", "1. 简要说明做了什么", "2. 当前状态（completed / waiting_approval / failed）", "3. 关键命令与关键结果摘要")
	return strings.Join(sections, "\n")
}
