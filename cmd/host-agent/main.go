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

	if err := stream.Send(&agentrpc.Envelope{
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
			case "error":
				log.Printf("server error: %s", msg.Error)
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
			if err := stream.Send(&agentrpc.Envelope{
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
