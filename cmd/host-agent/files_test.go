package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
)

func TestHandleAgentFileReadMarksResultNonCancelable(t *testing.T) {
	stream := &fakeAgentConnectClient{}
	sender := &agentStreamSender{stream: stream}
	dir := t.TempDir()
	path := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(path, []byte("worker_processes auto;\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if err := handleAgentFileRead(nil, sender, &agentrpc.FileReadRequest{
		RequestID: "fread-1",
		Path:      path,
		MaxBytes:  1024,
	}); err != nil {
		t.Fatalf("handleAgentFileRead: %v", err)
	}

	if len(stream.messages) != 1 || stream.messages[0].FileReadResult == nil {
		t.Fatalf("expected one file/read/result message, got %#v", stream.messages)
	}
	if stream.messages[0].FileReadResult.Cancelable {
		t.Fatalf("expected file read result to report cancelable=false, got %#v", stream.messages[0].FileReadResult)
	}
}

func TestHandleAgentFileWriteMarksResultNonCancelable(t *testing.T) {
	stream := &fakeAgentConnectClient{}
	sender := &agentStreamSender{stream: stream}
	dir := t.TempDir()
	path := filepath.Join(dir, "app.conf")

	if err := handleAgentFileWrite(nil, sender, &agentrpc.FileWriteRequest{
		RequestID: "fwrite-1",
		Path:      path,
		Content:   "server_name example.com;\n",
		WriteMode: "overwrite",
	}); err != nil {
		t.Fatalf("handleAgentFileWrite: %v", err)
	}

	if len(stream.messages) != 1 || stream.messages[0].FileWriteResult == nil {
		t.Fatalf("expected one file/write/result message, got %#v", stream.messages)
	}
	if stream.messages[0].FileWriteResult.Cancelable {
		t.Fatalf("expected file write result to report cancelable=false, got %#v", stream.messages[0].FileWriteResult)
	}
}
