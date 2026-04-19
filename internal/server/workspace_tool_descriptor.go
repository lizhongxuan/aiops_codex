package server

import "strings"

type workspaceToolDescriptorView struct {
	Name        string
	DisplayName string
	Kind        string
	Description string
	Aliases     []string
}

func (a *App) workspaceToolDescriptor(name string) workspaceToolDescriptorView {
	name = strings.TrimSpace(name)
	if name == "" {
		return workspaceToolDescriptorView{}
	}

	view := workspaceToolDescriptorView{
		Name:        name,
		DisplayName: workspaceToolFallbackDisplayName(name),
		Kind:        workspaceToolFallbackKind(name),
		Description: toolPromptDescription(name),
	}
	if a != nil && a.toolHandlerRegistry != nil {
		if desc, ok := a.toolHandlerRegistry.Descriptor(name); ok {
			if label := strings.TrimSpace(desc.DisplayLabel); label != "" {
				view.DisplayName = label
			}
			if kind := strings.TrimSpace(desc.Kind); kind != "" {
				view.Kind = kind
			}
		}
	}
	switch name {
	case "orchestrator_dispatch_tasks":
		view.Aliases = []string{"agent_tool", "dispatch_workers"}
	case "request_approval":
		view.Aliases = []string{"approval_tool"}
	}
	return view
}

func workspaceToolFallbackDisplayName(name string) string {
	switch strings.TrimSpace(name) {
	case "ask_user_question":
		return "澄清问题"
	case "command":
		return "命令执行"
	case "request_approval":
		return "审批请求"
	case "readonly_host_inspect":
		return "只读主机检查"
	case "query_ai_server_state":
		return "工作台状态快照"
	case "web_search":
		return "外部搜索"
	case "open_page":
		return "网页读取"
	case "find_in_page":
		return "页面定位"
	case "orchestrator_dispatch_tasks":
		return "任务派发"
	case "enter_plan_mode":
		return "进入计划模式"
	case "update_plan":
		return "计划更新"
	case "exit_plan_mode":
		return "计划审批"
	case "execute_system_mutation":
		return "受控变更"
	default:
		return name
	}
}

func workspaceToolFallbackKind(name string) string {
	switch strings.TrimSpace(name) {
	case "ask_user_question":
		return "question"
	case "query_ai_server_state":
		return "workspace_state"
	case "readonly_host_inspect", "command":
		return "command"
	case "enter_plan_mode", "update_plan":
		return "plan"
	case "exit_plan_mode", "request_approval":
		return "approval"
	case "orchestrator_dispatch_tasks":
		return "agent"
	case "execute_system_mutation":
		return "mutation"
	default:
		return ""
	}
}
