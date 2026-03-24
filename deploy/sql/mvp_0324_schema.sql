-- mvp_0324 logical schema
-- The runtime still uses the in-memory store plus APP_STATE_PATH persistence.
-- This file defines the first database shape for moving the MVP to durable storage.

CREATE TABLE IF NOT EXISTS web_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS codex_auth_sessions (
  id TEXT PRIMARY KEY,
  web_user_id TEXT,
  auth_mode TEXT NOT NULL,
  email TEXT,
  plan_type TEXT,
  connected INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  access_token_encrypted TEXT,
  id_token_encrypted TEXT,
  chatgpt_account_id TEXT,
  chatgpt_plan_type TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (web_user_id) REFERENCES web_users(id)
);

CREATE TABLE IF NOT EXISTS web_sessions (
  id TEXT PRIMARY KEY,
  web_user_id TEXT,
  auth_session_id TEXT,
  selected_host_id TEXT,
  last_activity_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (web_user_id) REFERENCES web_users(id),
  FOREIGN KEY (auth_session_id) REFERENCES codex_auth_sessions(id)
);

CREATE TABLE IF NOT EXISTS codex_threads (
  id TEXT PRIMARY KEY,
  web_session_id TEXT NOT NULL,
  thread_id TEXT NOT NULL UNIQUE,
  selected_host_id TEXT,
  cwd TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (web_session_id) REFERENCES web_sessions(id)
);

CREATE TABLE IF NOT EXISTS approval_requests (
  id TEXT PRIMARY KEY,
  web_session_id TEXT NOT NULL,
  thread_id TEXT NOT NULL,
  turn_id TEXT,
  item_id TEXT,
  request_type TEXT NOT NULL,
  status TEXT NOT NULL,
  decision TEXT,
  command_text TEXT,
  cwd TEXT,
  grant_root TEXT,
  reason TEXT,
  payload_json TEXT,
  requested_at TEXT NOT NULL,
  resolved_at TEXT,
  FOREIGN KEY (web_session_id) REFERENCES web_sessions(id)
);

CREATE TABLE IF NOT EXISTS hosts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  executable INTEGER NOT NULL DEFAULT 0,
  os TEXT,
  arch TEXT,
  agent_version TEXT,
  labels_json TEXT,
  last_heartbeat_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS host_agent_connections (
  id TEXT PRIMARY KEY,
  host_id TEXT NOT NULL,
  connection_state TEXT NOT NULL,
  remote_addr TEXT,
  grpc_peer TEXT,
  bootstrap_subject TEXT,
  connected_at TEXT NOT NULL,
  last_heartbeat_at TEXT,
  disconnected_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (host_id) REFERENCES hosts(id)
);

CREATE INDEX IF NOT EXISTS idx_web_sessions_auth_session_id
  ON web_sessions(auth_session_id);

CREATE INDEX IF NOT EXISTS idx_web_sessions_selected_host_id
  ON web_sessions(selected_host_id);

CREATE INDEX IF NOT EXISTS idx_codex_threads_web_session_id
  ON codex_threads(web_session_id);

CREATE INDEX IF NOT EXISTS idx_approval_requests_web_session_id
  ON approval_requests(web_session_id);

CREATE INDEX IF NOT EXISTS idx_approval_requests_status
  ON approval_requests(status);

CREATE INDEX IF NOT EXISTS idx_approval_requests_request_type
  ON approval_requests(request_type);

CREATE INDEX IF NOT EXISTS idx_hosts_status
  ON hosts(status);

CREATE INDEX IF NOT EXISTS idx_host_agent_connections_host_id
  ON host_agent_connections(host_id);
