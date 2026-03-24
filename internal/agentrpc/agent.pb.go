// Code generated from proto/agent.proto. DO NOT EDIT.

package agentrpc

type Registration struct {
	Token        string            `json:"token,omitempty"`
	HostID       string            `json:"hostId,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
	OS           string            `json:"os,omitempty"`
	Arch         string            `json:"arch,omitempty"`
	AgentVersion string            `json:"agentVersion,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type Heartbeat struct {
	HostID    string `json:"hostId,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Ping struct {
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Ack struct {
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Envelope struct {
	Kind         string        `json:"kind"`
	Registration *Registration `json:"registration,omitempty"`
	Heartbeat    *Heartbeat    `json:"heartbeat,omitempty"`
	Ping         *Ping         `json:"ping,omitempty"`
	Ack          *Ack          `json:"ack,omitempty"`
	Error        string        `json:"error,omitempty"`
}
