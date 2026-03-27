export const mockHosts = [
  {
    id: "host-prod-07",
    name: "web-07",
    kind: "agent",
    status: "online",
    executable: true,
    terminalCapable: true,
    os: "Ubuntu 22.04",
    arch: "x86_64",
    agentVersion: "1.8.4",
    lastHeartbeat: "2s ago",
    labels: { env: "prod", role: "web", app: "nginx" },
    recentActivity: [
      "14:30 nginx reload 成功",
      "14:25 配置检查完成",
      "14:12 主 Agent 派发诊断任务",
      "13:58 历史会话 9 条",
    ],
  },
  {
    id: "host-prod-08",
    name: "web-08",
    kind: "agent",
    status: "online",
    executable: true,
    terminalCapable: true,
    os: "Ubuntu 22.04",
    arch: "x86_64",
    agentVersion: "1.8.4",
    lastHeartbeat: "4s ago",
    labels: { env: "prod", role: "web", app: "nginx" },
    recentActivity: [
      "14:29 命令待审批",
      "14:24 配置热加载检查",
      "14:10 主 Agent 任务接入",
      "13:50 最近会话 7 条",
    ],
  },
  {
    id: "host-api-03",
    name: "api-03",
    kind: "agent",
    status: "online",
    executable: true,
    terminalCapable: true,
    os: "Debian 12",
    arch: "x86_64",
    agentVersion: "1.8.2",
    lastHeartbeat: "11s ago",
    labels: { env: "prod", role: "api" },
    recentActivity: [
      "14:16 Java 诊断任务完成",
      "14:04 Coroot 指标拉取",
      "13:57 最近会话 4 条",
    ],
  },
  {
    id: "host-db-01",
    name: "db-01",
    kind: "agent",
    status: "online",
    executable: false,
    terminalCapable: true,
    os: "CentOS 7",
    arch: "x86_64",
    agentVersion: "1.7.9",
    lastHeartbeat: "18s ago",
    labels: { env: "prod", role: "db" },
    recentActivity: [
      "14:18 只读排查会话",
      "13:42 终端接入成功",
      "13:08 最近会话 3 条",
    ],
  },
  {
    id: "host-cache-04",
    name: "cache-04",
    kind: "agent",
    status: "online",
    executable: true,
    terminalCapable: true,
    os: 'AlmaLinux 9',
    arch: "x86_64",
    agentVersion: "1.8.3",
    lastHeartbeat: "26s ago",
    labels: { env: "prod", role: "cache" },
    recentActivity: [
      "14:09 Redis 诊断完成",
      "13:48 配置扫描更新",
      "13:10 最近会话 5 条",
    ],
  },
  {
    id: "host-legacy-02",
    name: "legacy-02",
    kind: "legacy",
    status: "offline",
    executable: false,
    terminalCapable: false,
    os: "RHEL 7",
    arch: "x86_64",
    agentVersion: "1.6.4",
    lastHeartbeat: "offline",
    labels: { env: "legacy" },
    recentActivity: [
      "12:32 心跳超时",
      "11:58 上次成功接入",
    ],
  },
];

export const experiencePacks = [
  {
    id: "nginx-reload",
    name: "Nginx 灰度重载恢复",
    summary: "适用于 Ubuntu / Debian Web 集群",
    version: "v3.2",
    confidence: "92% 成功率",
    confidenceTone: "success",
    status: "verified",
    statusTone: "success",
    risk: "low risk",
    platform: "linux",
    bindings: "绑定 3 个 playbook · 关联 11 台主机",
    purpose: "面向 Nginx 配置变更后的平滑 reload、失败回滚与健康检查。",
    versionTrail: [
      { label: "v1", state: "idle" },
      { label: "v2", state: "active" },
      { label: "v3.2", state: "success" },
    ],
    versionNote: "新增回滚守卫与批量执行节流",
    resources: [
      "playbook/nginx/reload-safe.yaml",
      "playbook/nginx/check-health.yaml",
      "host-profile:web-cluster",
      "memory:nginx-reload-failover",
    ],
  },
  {
    id: "java-memory",
    name: "Java 进程高内存排障",
    summary: "附带 Coroot 指标查询与线程 dump",
    version: "v2.8",
    confidence: "需要审批",
    confidenceTone: "warning",
    status: "reviewed",
    statusTone: "warning",
    risk: "medium risk",
    platform: "linux",
    bindings: "绑定 2 个 playbook · 关联 6 台主机",
    purpose: "处理 JVM 内存飙升、线程阻塞与 dump 导出流程。",
    versionTrail: [
      { label: "v1.4", state: "idle" },
      { label: "v2.1", state: "active" },
      { label: "v2.8", state: "warning" },
    ],
    versionNote: "增强线程 dump 门控与批量告警摘要",
    resources: [
      "playbook/java/heap-check.yaml",
      "tool:coroot/jvm-hot-path",
      "memory:jvm-thread-stall",
    ],
  },
  {
    id: "redis-failover",
    name: "Redis 只读故障切换",
    summary: "适用于单主从 Redis 服务",
    version: "v1.9",
    confidence: "84% 成功率",
    confidenceTone: "info",
    status: "verified",
    statusTone: "success",
    risk: "medium risk",
    platform: "linux",
    bindings: "绑定 1 个 playbook · 关联 4 台主机",
    purpose: "用于 Redis 只读异常、主从切换确认与回滚守卫。",
    versionTrail: [
      { label: "v1.0", state: "idle" },
      { label: "v1.6", state: "active" },
      { label: "v1.9", state: "info" },
    ],
    versionNote: "补充 sentinel 前置检查与回切确认",
    resources: [
      "playbook/redis/promote-replica.yaml",
      "memory:redis-readonly-failover",
    ],
  },
  {
    id: "disk-relief",
    name: "磁盘爆满快速止血",
    summary: "日志清理 + 容量分析 + 回滚提示",
    version: "v4.0",
    confidence: "广泛验证",
    confidenceTone: "success",
    status: "verified",
    statusTone: "success",
    risk: "low risk",
    platform: "linux",
    bindings: "绑定 4 个 playbook · 关联 18 台主机",
    purpose: "优先快速释放空间，再补充定位与结构化排查步骤。",
    versionTrail: [
      { label: "v2.0", state: "idle" },
      { label: "v3.1", state: "active" },
      { label: "v4.0", state: "success" },
    ],
    versionNote: "追加容量阈值守卫和日志采样清理",
    resources: [
      "playbook/fs/check-capacity.yaml",
      "playbook/fs/trim-logs.yaml",
    ],
  },
  {
    id: "k8s-restore",
    name: "K8s 节点不可调度恢复",
    summary: "Node cordon / drain / kubelet 检查",
    version: "v1.4",
    confidence: "新版本",
    confidenceTone: "purple",
    status: "preview",
    statusTone: "purple",
    risk: "medium risk",
    platform: "linux",
    bindings: "绑定 2 个 playbook · 关联 9 台主机",
    purpose: "针对节点不可调度、驱逐失败与 kubelet 异常提供分步恢复路径。",
    versionTrail: [
      { label: "v0.9", state: "idle" },
      { label: "v1.2", state: "active" },
      { label: "v1.4", state: "purple" },
    ],
    versionNote: "新增 drain 失败回退建议",
    resources: [
      "playbook/k8s/node-drain.yaml",
      "memory:kubelet-not-ready",
    ],
  },
];

export const protocolContext = {
  request:
    "请在今晚发布前检查 web 集群 nginx 配置，并在异常时自动修复。",
  planner:
    "1. 查询经验包与主机画像\n2. 生成分批 DAG\n3. 将 reload 检查派发至 3 台主机\n4. 对失败节点执行 fallback",
  attachments: [
    "Pack: nginx-reload-v3.2",
    "Host Group: web-cluster",
    "Policy: batch-size=3",
    "Approval Mode: batch-approve",
    "Coroot RCA: enabled",
  ],
};

export const protocolNodes = [
  { id: "observe", label: "observe", detail: "completed", status: "completed", x: 76, y: 32, w: 126, h: 62 },
  { id: "analyze", label: "analyze", detail: "completed", status: "completed", x: 250, y: 32, w: 126, h: 62 },
  { id: "plan", label: "plan", detail: "running", status: "running", x: 166, y: 154, w: 126, h: 72 },
  { id: "exec-07", label: "execute · web-07", detail: "completed", status: "completed", x: 4, y: 294, w: 144, h: 84 },
  { id: "exec-08", label: "execute · web-08", detail: "waiting approval", status: "warning", x: 182, y: 294, w: 144, h: 84 },
  { id: "exec-09", label: "execute · web-09", detail: "running", status: "running", x: 360, y: 294, w: 144, h: 84 },
  { id: "learn", label: "verify + learn", detail: "pending", status: "pending", x: 198, y: 440, w: 144, h: 72 },
];

export const protocolLanes = [
  {
    id: "web-07",
    hostId: "host-prod-07",
    title: "web-07",
    status: "已完成",
    tone: "success",
    summary: "nginx -t 通过，reload 已完成",
    meta: "耗时 18s · 0 风险",
  },
  {
    id: "web-08",
    hostId: "host-prod-08",
    title: "web-08",
    status: "等待审批",
    tone: "warning",
    summary: "命令待批: nginx -s reload",
    meta: "需要同批策略确认",
  },
  {
    id: "web-09",
    hostId: "host-prod-09",
    title: "web-09",
    status: "执行中",
    tone: "info",
    summary: "正在检查 upstream 与健康探针",
    meta: "最近输出: 3/5 checks passed",
  },
];

export function labelList(labels) {
  if (!labels) return [];
  if (Array.isArray(labels)) return labels;
  return Object.entries(labels)
    .filter(([key]) => key)
    .map(([key, value]) => (value ? `${key}:${value}` : key));
}

export function normalizeHostRecord(host, index = 0) {
  const fallback = mockHosts[index % mockHosts.length];
  return {
    id: host.id,
    name: host.name || host.id,
    kind: host.kind || fallback.kind,
    address: host.address || "",
    transport: host.transport || "",
    status: host.status || "offline",
    executable: Boolean(host.executable),
    terminalCapable: Boolean(host.terminalCapable),
    os: host.os || fallback.os,
    arch: host.arch || fallback.arch,
    agentVersion: host.agentVersion || fallback.agentVersion,
    lastHeartbeat: host.lastHeartbeat || (host.status === "offline" ? "offline" : fallback.lastHeartbeat),
    labels: host.labels || fallback.labels,
    lastError: host.lastError || "",
    sshUser: host.sshUser || "",
    sshPort: host.sshPort || 22,
    installState: host.installState || "",
    controlMode: host.controlMode || "",
    recentActivity: fallback.recentActivity,
  };
}

export function hostCapabilityLabel(host) {
  if (host.executable && host.terminalCapable) return "exec + term";
  if (host.executable) return "exec only";
  if (host.terminalCapable) return "term only";
  return "read only";
}

export function toneClass(tone) {
  switch (tone) {
    case "success":
      return "is-success";
    case "warning":
      return "is-warning";
    case "info":
      return "is-info";
    case "active":
      return "is-info";
    case "purple":
      return "is-purple";
    default:
      return "is-neutral";
  }
}
