package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := env("AIOPS_SERVER_GRPC_ADDR", "127.0.0.1:19090")
	hostID := env("AIOPS_AGENT_HOST_ID", hostName())
	hostname := env("AIOPS_AGENT_HOSTNAME", hostName())
	version := env("AIOPS_AGENT_VERSION", "0.1.0")
	token := env("AIOPS_AGENT_BOOTSTRAP_TOKEN", "change-me")
	labels := parseLabels(env("AIOPS_AGENT_LABELS", "env=dev"))

	for {
		if err := run(ctx, addr, hostID, hostname, version, token, labels); err != nil {
			log.Printf("agent disconnected: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func run(ctx context.Context, addr, hostID, hostname, version, token string, labels map[string]string) error {
	conn, err := server.DialAgent(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := agentrpc.NewAgentServiceClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	sender := &agentStreamSender{stream: stream}
	agentRuntime, err := newHostAgentRuntime()
	if err != nil {
		return err
	}
	terminals := newAgentTerminalManager(sender, agentRuntime)
	execs := newAgentExecManager(sender, agentRuntime)
	defer terminals.shutdownAll()
	defer execs.shutdownAll()

	if err := sender.send(&agentrpc.Envelope{
		Kind: "register",
		Registration: &agentrpc.Registration{
			Token:        token,
			HostID:       hostID,
			Hostname:     hostname,
			OS:           runtime.GOOS,
			Arch:         runtime.GOARCH,
			AgentVersion: version,
			Labels:       labels,
		},
	}); err != nil {
		return err
	}
	profile, revision, unsupported := agentRuntime.profile.snapshot()
	_ = sender.send(profileAckEnvelope(hostAgentProfileAckMessage{
		ProfileID:   profile.ID,
		Revision:    revision,
		Status:      "loaded",
		Summary:     hostAgentProfileSummary(profile, unsupported) + " (unsupported runtime-only fields: " + strings.Join(unsupported, ", ") + ")",
		Unsupported: unsupported,
	}))

	errCh := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			switch msg.Kind {
			case "ack":
				if msg.Ack != nil {
					log.Printf("server ack: %s", msg.Ack.Message)
				}
			case hostAgentProfileUpdateKind:
				ack, err := agentRuntime.profile.applyUpdate(msg)
				if err != nil {
					log.Printf("profile update failed: %v", err)
					_ = sender.send(profileAckErrorEnvelope(err.Error()))
					continue
				}
				log.Printf("profile update applied: rev=%s status=%s", ack.Revision, ack.Status)
				_ = sender.send(profileAckEnvelope(ack))
			case "error":
				log.Printf("server error: %s", msg.Error)
			case "terminal/open":
				if msg.TerminalOpen != nil {
					log.Printf("recv terminal/open session=%s cwd=%q shell=%q", msg.TerminalOpen.SessionID, msg.TerminalOpen.Cwd, msg.TerminalOpen.Shell)
				}
				if err := terminals.open(msg.TerminalOpen); err != nil {
					_ = sender.send(&agentrpc.Envelope{
						Kind: "terminal/status",
						TerminalStatus: &agentrpc.TerminalStatus{
							SessionID: safeTerminalSessionID(msg.TerminalOpen),
							Status:    "error",
							Message:   err.Error(),
						},
					})
				}
			case "terminal/input":
				if msg.TerminalInput != nil {
					log.Printf("recv terminal/input session=%s bytes=%d data=%q", msg.TerminalInput.SessionID, len(msg.TerminalInput.Data), summarizeLogText(msg.TerminalInput.Data, 240))
				}
				if err := terminals.input(msg.TerminalInput); err != nil {
					_ = sender.send(&agentrpc.Envelope{
						Kind: "terminal/status",
						TerminalStatus: &agentrpc.TerminalStatus{
							SessionID: safeTerminalSessionIDFromInput(msg.TerminalInput),
							Status:    "error",
							Message:   err.Error(),
						},
					})
				}
			case "terminal/resize":
				if msg.TerminalResize != nil {
					log.Printf("recv terminal/resize session=%s cols=%d rows=%d", msg.TerminalResize.SessionID, msg.TerminalResize.Cols, msg.TerminalResize.Rows)
				}
				if err := terminals.resize(msg.TerminalResize); err != nil {
					_ = sender.send(&agentrpc.Envelope{
						Kind: "terminal/status",
						TerminalStatus: &agentrpc.TerminalStatus{
							SessionID: safeTerminalSessionIDFromResize(msg.TerminalResize),
							Status:    "error",
							Message:   err.Error(),
						},
					})
				}
			case "terminal/signal":
				if msg.TerminalSignal != nil {
					log.Printf("recv terminal/signal session=%s signal=%q", msg.TerminalSignal.SessionID, msg.TerminalSignal.Signal)
				}
				if err := terminals.signal(msg.TerminalSignal); err != nil {
					_ = sender.send(&agentrpc.Envelope{
						Kind: "terminal/status",
						TerminalStatus: &agentrpc.TerminalStatus{
							SessionID: safeTerminalSessionIDFromSignal(msg.TerminalSignal),
							Status:    "error",
							Message:   err.Error(),
						},
					})
				}
			case "terminal/close":
				if msg.TerminalClose != nil {
					log.Printf("recv terminal/close session=%s", msg.TerminalClose.SessionID)
				}
				if err := terminals.close(msg.TerminalClose); err != nil {
					_ = sender.send(&agentrpc.Envelope{
						Kind: "terminal/status",
						TerminalStatus: &agentrpc.TerminalStatus{
							SessionID: safeTerminalSessionIDFromClose(msg.TerminalClose),
							Status:    "error",
							Message:   err.Error(),
						},
					})
				}
			case "exec/start":
				go func(req *agentrpc.ExecStart) {
					if req != nil {
						log.Printf("recv exec/start exec=%s readonly=%t timeout=%ds cwd=%q shell=%q command=%q", req.ExecID, req.Readonly, req.TimeoutSec, req.Cwd, req.Shell, summarizeLogText(req.Command, 320))
					}
					if err := execs.start(req); err != nil {
						log.Printf("exec/start failed exec=%s err=%v", safeExecID(req), err)
						_ = sender.send(&agentrpc.Envelope{
							Kind: "exec/exit",
							ExecExit: &agentrpc.ExecExit{
								ExecID:   safeExecID(req),
								Code:     1,
								ExitCode: 1,
								Status:   "failed",
								Message:  err.Error(),
								Error:    err.Error(),
							},
						})
					}
				}(msg.ExecStart)
			case "exec/cancel":
				if msg.ExecCancel != nil {
					log.Printf("recv exec/cancel exec=%s", msg.ExecCancel.ExecID)
				}
				if err := execs.cancel(msg.ExecCancel); err != nil {
					_ = sender.send(&agentrpc.Envelope{
						Kind: "exec/exit",
						ExecExit: &agentrpc.ExecExit{
							ExecID:   safeExecIDFromCancel(msg.ExecCancel),
							Code:     1,
							ExitCode: 1,
							Status:   "failed",
							Message:  err.Error(),
							Error:    err.Error(),
						},
					})
				}
			case "file/list":
				go func(req *agentrpc.FileListRequest) {
					if req != nil {
						log.Printf("recv file/list request=%s path=%q recursive=%t max_entries=%d", req.RequestID, req.Path, req.Recursive, req.MaxEntries)
					}
					if err := handleAgentFileList(agentRuntime, sender, req); err != nil {
						log.Printf("file/list failed request=%s err=%v", safeFileRequestIDFromList(req), err)
						_ = sender.send(&agentrpc.Envelope{
							Kind: "file/list/result",
							FileListResult: &agentrpc.FileListResult{
								RequestID: safeFileRequestIDFromList(req),
								Message:   err.Error(),
							},
						})
					}
				}(msg.FileListRequest)
			case "file/read":
				go func(req *agentrpc.FileReadRequest) {
					if req != nil {
						log.Printf("recv file/read request=%s path=%q max_bytes=%d", req.RequestID, req.Path, req.MaxBytes)
					}
					if err := handleAgentFileRead(agentRuntime, sender, req); err != nil {
						log.Printf("file/read failed request=%s err=%v", safeFileRequestIDFromRead(req), err)
						_ = sender.send(&agentrpc.Envelope{
							Kind: "file/read/result",
							FileReadResult: &agentrpc.FileReadResult{
								RequestID: safeFileRequestIDFromRead(req),
								Message:   err.Error(),
							},
						})
					}
				}(msg.FileReadRequest)
			case "file/search":
				go func(req *agentrpc.FileSearchRequest) {
					if req != nil {
						log.Printf("recv file/search request=%s path=%q query=%q max_matches=%d", req.RequestID, req.Path, summarizeLogText(req.Query, 200), req.MaxMatches)
					}
					if err := handleAgentFileSearch(agentRuntime, sender, req); err != nil {
						log.Printf("file/search failed request=%s err=%v", safeFileRequestIDFromSearch(req), err)
						_ = sender.send(&agentrpc.Envelope{
							Kind: "file/search/result",
							FileSearchResult: &agentrpc.FileSearchResult{
								RequestID: safeFileRequestIDFromSearch(req),
								Message:   err.Error(),
							},
						})
					}
				}(msg.FileSearchRequest)
			case "file/write":
				go func(req *agentrpc.FileWriteRequest) {
					if req != nil {
						log.Printf("recv file/write request=%s path=%q mode=%q content=%q", req.RequestID, req.Path, req.WriteMode, summarizeLogText(req.Content, 240))
					}
					if err := handleAgentFileWrite(agentRuntime, sender, req); err != nil {
						log.Printf("file/write failed request=%s err=%v", safeFileRequestIDFromWrite(req), err)
						_ = sender.send(&agentrpc.Envelope{
							Kind: "file/write/result",
							FileWriteResult: &agentrpc.FileWriteResult{
								RequestID: safeFileRequestIDFromWrite(req),
								Message:   err.Error(),
							},
						})
					}
				}(msg.FileWriteRequest)
			}
		}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-ticker.C:
			if err := sender.send(&agentrpc.Envelope{
				Kind: "heartbeat",
				Heartbeat: &agentrpc.Heartbeat{
					HostID:    hostID,
					Timestamp: time.Now().Unix(),
				},
			}); err != nil {
				return err
			}
		}
	}
}

func safeTerminalSessionID(req *agentrpc.TerminalOpen) string {
	if req == nil {
		return ""
	}
	return req.SessionID
}

func safeTerminalSessionIDFromInput(req *agentrpc.TerminalInput) string {
	if req == nil {
		return ""
	}
	return req.SessionID
}

func safeTerminalSessionIDFromResize(req *agentrpc.TerminalResize) string {
	if req == nil {
		return ""
	}
	return req.SessionID
}

func safeTerminalSessionIDFromSignal(req *agentrpc.TerminalSignal) string {
	if req == nil {
		return ""
	}
	return req.SessionID
}

func safeTerminalSessionIDFromClose(req *agentrpc.TerminalClose) string {
	if req == nil {
		return ""
	}
	return req.SessionID
}

func safeExecID(req *agentrpc.ExecStart) string {
	if req == nil {
		return ""
	}
	return req.ExecID
}

func safeExecIDFromCancel(req *agentrpc.ExecCancel) string {
	if req == nil {
		return ""
	}
	return req.ExecID
}

func safeFileRequestIDFromList(req *agentrpc.FileListRequest) string {
	if req == nil {
		return ""
	}
	return req.RequestID
}

func safeFileRequestIDFromRead(req *agentrpc.FileReadRequest) string {
	if req == nil {
		return ""
	}
	return req.RequestID
}

func safeFileRequestIDFromSearch(req *agentrpc.FileSearchRequest) string {
	if req == nil {
		return ""
	}
	return req.RequestID
}

func safeFileRequestIDFromWrite(req *agentrpc.FileWriteRequest) string {
	if req == nil {
		return ""
	}
	return req.RequestID
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown-host"
	}
	return name
}

func parseLabels(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items := strings.SplitN(part, "=", 2)
		if len(items) != 2 {
			continue
		}
		out[strings.TrimSpace(items[0])] = strings.TrimSpace(items[1])
	}
	return out
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
