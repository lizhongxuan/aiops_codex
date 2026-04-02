package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lizhongxuan/aiops-codex/internal/agentrpc"
	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/model"
	"google.golang.org/grpc/metadata"
)

type fileChangeAgentStream struct {
	mu       sync.Mutex
	messages []*agentrpc.Envelope
	onSend   func(*agentrpc.Envelope) error
}

func (s *fileChangeAgentStream) SetHeader(_ metadata.MD) error { return nil }

func (s *fileChangeAgentStream) SendHeader(_ metadata.MD) error { return nil }

func (s *fileChangeAgentStream) SetTrailer(_ metadata.MD) {}

func (s *fileChangeAgentStream) Context() context.Context { return context.Background() }

func (s *fileChangeAgentStream) Send(msg *agentrpc.Envelope) error {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
	if s.onSend != nil {
		return s.onSend(msg)
	}
	return nil
}

func (s *fileChangeAgentStream) Recv() (*agentrpc.Envelope, error) { return nil, io.EOF }

func (s *fileChangeAgentStream) SendMsg(any) error { return nil }

func (s *fileChangeAgentStream) RecvMsg(any) error { return io.EOF }

func (s *fileChangeAgentStream) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kinds := make([]string, 0, len(s.messages))
	for _, msg := range s.messages {
		kinds = append(kinds, msg.Kind)
	}
	return kinds
}

func TestValidateRemoteFileChangeArgumentsRequiresExplicitProtocol(t *testing.T) {
	t.Run("success with empty content allowed", func(t *testing.T) {
		err := validateRemoteFileChangeArguments(map[string]any{
			"host":       "linux-01",
			"mode":       "file_change",
			"path":       "/etc/app.conf",
			"content":    "",
			"write_mode": "overwrite",
			"reason":     "clear file",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	cases := []struct {
		name    string
		args    map[string]any
		wantMsg string
	}{
		{
			name: "missing host",
			args: map[string]any{
				"mode":       "file_change",
				"path":       "/etc/app.conf",
				"content":    "x",
				"write_mode": "overwrite",
				"reason":     "update",
			},
			wantMsg: "file_change requires host",
		},
		{
			name: "missing mode",
			args: map[string]any{
				"host":       "linux-01",
				"path":       "/etc/app.conf",
				"content":    "x",
				"write_mode": "overwrite",
				"reason":     "update",
			},
			wantMsg: "file_change requires mode=file_change",
		},
		{
			name: "missing content",
			args: map[string]any{
				"host":       "linux-01",
				"mode":       "file_change",
				"path":       "/etc/app.conf",
				"write_mode": "overwrite",
				"reason":     "update",
			},
			wantMsg: "file_change requires content",
		},
		{
			name: "missing write_mode",
			args: map[string]any{
				"host":    "linux-01",
				"mode":    "file_change",
				"path":    "/etc/app.conf",
				"content": "x",
				"reason":  "update",
			},
			wantMsg: "file_change requires write_mode",
		},
		{
			name: "missing path",
			args: map[string]any{
				"host":       "linux-01",
				"mode":       "file_change",
				"content":    "x",
				"write_mode": "overwrite",
				"reason":     "update",
			},
			wantMsg: "file_change requires a path",
		},
		{
			name: "missing reason",
			args: map[string]any{
				"host":       "linux-01",
				"mode":       "file_change",
				"path":       "/etc/app.conf",
				"content":    "x",
				"write_mode": "overwrite",
			},
			wantMsg: "file_change requires a reason",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRemoteFileChangeArguments(tc.args)
			if err == nil {
				t.Fatalf("expected error")
			}
			if err.Error() != tc.wantMsg {
				t.Fatalf("expected %q, got %q", tc.wantMsg, err.Error())
			}
		})
	}
}

func TestRequestRemoteFileChangeApprovalDoesNotWriteBeforeApproval(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-file-change-approval"
	hostID := "linux-01"
	readCount := 0
	writeCount := 0

	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})

	stream := &fileChangeAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			switch {
			case msg.FileReadRequest != nil:
				readCount++
				app.handleAgentFileReadResult(hostID, &agentrpc.FileReadResult{
					RequestID: msg.FileReadRequest.RequestID,
					Path:      msg.FileReadRequest.Path,
					Content:   "old-value\n",
					Message:   "no such file or directory",
				})
			case msg.FileWriteRequest != nil:
				writeCount++
				app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
					RequestID:  msg.FileWriteRequest.RequestID,
					Path:       msg.FileWriteRequest.Path,
					NewContent: msg.FileWriteRequest.Content,
					WriteMode:  msg.FileWriteRequest.WriteMode,
				})
			}
			return nil
		},
	}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	app.requestRemoteFileChangeApproval(sessionID, hostID, "raw-file-change", dynamicToolCallParams{
		ThreadID: "thread-file-change",
		TurnID:   "turn-file-change",
		CallID:   "call-file-change",
		Tool:     "execute_system_mutation",
	}, remoteFileChangeArgs{
		HostID:    hostID,
		Mode:      "file_change",
		Path:      "/etc/app.conf",
		Content:   "new-value\n",
		WriteMode: "overwrite",
		Reason:    "update config",
	})

	if readCount != 1 {
		t.Fatalf("expected one file read before approval, got %d", readCount)
	}
	if writeCount != 0 {
		t.Fatalf("expected no file write before approval, got %d", writeCount)
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	foundApproval := false
	for _, card := range session.Cards {
		if card.Type == "FileChangeApprovalCard" {
			foundApproval = true
			if len(card.Changes) == 0 || card.Changes[0].Kind != "create" {
				t.Fatalf("expected missing file to be represented as create, got %#v", card.Changes)
			}
		}
	}
	if !foundApproval {
		t.Fatalf("expected file change approval card to exist")
	}
	if session.Runtime.Turn.Phase != "waiting_approval" {
		t.Fatalf("expected session to wait for approval, got %q", session.Runtime.Turn.Phase)
	}
	if got := len(stream.kinds()); got == 0 {
		t.Fatalf("expected at least one agent request")
	}
}

func TestExecuteApprovedRemoteFileChangeHandlesOverwriteAndAppend(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		writeMode string
		response  *agentrpc.FileWriteResult
		wantKind  string
		wantText  string
		wantDiff  string
	}{
		{
			name:      "overwrite",
			content:   "new-value\n",
			writeMode: "overwrite",
			response: &agentrpc.FileWriteResult{
				Path:       "/etc/app.conf",
				OldContent: "old-value\n",
				NewContent: "new-value\n",
				Created:    false,
				WriteMode:  "overwrite",
			},
			wantKind: "update",
			wantText: "已修改远程文件 /etc/app.conf",
			wantDiff: "new-value",
		},
		{
			name:      "append",
			content:   "extra-line\n",
			writeMode: "append",
			response: &agentrpc.FileWriteResult{
				Path:       "/etc/app.conf",
				OldContent: "base-value\n",
				NewContent: "base-value\nextra-line\n",
				Created:    false,
				WriteMode:  "append",
			},
			wantKind: "append",
			wantText: "已修改远程文件 /etc/app.conf",
			wantDiff: "extra-line",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := New(config.Config{})
			sessionID := "sess-file-change-" + tc.name
			hostID := "linux-01"
			approvalID := "approval-" + tc.name
			itemID := "item-" + tc.name
			now := model.NowString()
			writeCount := 0

			app.store.EnsureSession(sessionID)
			app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})
			app.store.RememberItem(sessionID, itemID, map[string]any{
				"host":       hostID,
				"mode":       "file_change",
				"path":       "/etc/app.conf",
				"content":    tc.content,
				"write_mode": tc.writeMode,
				"writeMode":  tc.writeMode,
				"reason":     "update config",
			})
			app.store.AddApproval(sessionID, model.ApprovalRequest{
				ID:           approvalID,
				RequestIDRaw: "raw-" + tc.name,
				HostID:       hostID,
				Type:         "remote_file_change",
				Status:       "accepted",
				ThreadID:     "thread-" + tc.name,
				TurnID:       "turn-" + tc.name,
				ItemID:       itemID,
				Reason:       "update config",
				Changes:      []model.FileChange{{Path: "/etc/app.conf", Kind: "update", Diff: "placeholder"}},
				RequestedAt:  now,
			})
			app.store.UpsertCard(sessionID, model.Card{
				ID:        itemID,
				Type:      "FileChangeApprovalCard",
				Status:    "pending",
				CreatedAt: now,
				UpdatedAt: now,
			})

			stream := &fileChangeAgentStream{
				onSend: func(msg *agentrpc.Envelope) error {
					if msg.FileWriteRequest != nil {
						writeCount++
						if msg.FileWriteRequest.WriteMode != tc.writeMode {
							t.Fatalf("expected write_mode %q, got %q", tc.writeMode, msg.FileWriteRequest.WriteMode)
						}
						result := *tc.response
						result.RequestID = msg.FileWriteRequest.RequestID
						result.Path = msg.FileWriteRequest.Path
						result.WriteMode = msg.FileWriteRequest.WriteMode
						app.handleAgentFileWriteResult(hostID, &result)
					}
					return nil
				},
			}
			app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

			approval, ok := app.store.Approval(sessionID, approvalID)
			if !ok {
				t.Fatalf("expected approval to exist")
			}
			app.executeApprovedRemoteFileChange(sessionID, approval)

			if writeCount != 1 {
				t.Fatalf("expected exactly one file write, got %d", writeCount)
			}

			card := app.cardByID(sessionID, itemID)
			if card == nil {
				t.Fatalf("expected card to exist")
			}
			if card.Status != "completed" {
				t.Fatalf("expected completed card, got %q", card.Status)
			}
			if len(card.Changes) != 1 || card.Changes[0].Kind != tc.wantKind {
				t.Fatalf("expected change kind %q, got %#v", tc.wantKind, card.Changes)
			}
			if !strings.Contains(card.Text, tc.wantText) {
				t.Fatalf("expected card text to mention file path, got %q", card.Text)
			}
			if !strings.Contains(card.Changes[0].Diff, tc.wantDiff) {
				t.Fatalf("expected diff to include %q, got %q", tc.wantDiff, card.Changes[0].Diff)
			}
		})
	}
}

func TestExecuteApprovedRemoteFileChangeReportsPermissionAndIOErrors(t *testing.T) {
	cases := []struct {
		name      string
		message   string
		wantParts []string
	}{
		{
			name:      "permission denied",
			message:   "permission denied",
			wantParts: []string{"file_change failed", "permission denied"},
		},
		{
			name:      "path not found",
			message:   "no such file or directory",
			wantParts: []string{"file_change failed", "path not found"},
		},
		{
			name:      "io error",
			message:   "input/output error",
			wantParts: []string{"file_change failed", "i/o error"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := New(config.Config{})
			sessionID := "sess-file-change-error-" + tc.name
			hostID := "linux-01"
			approvalID := "approval-" + tc.name
			itemID := "item-" + tc.name
			now := model.NowString()

			app.store.EnsureSession(sessionID)
			app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})
			app.store.RememberItem(sessionID, itemID, map[string]any{
				"host":       hostID,
				"mode":       "file_change",
				"path":       "/etc/app.conf",
				"content":    "new-value\n",
				"write_mode": "overwrite",
				"writeMode":  "overwrite",
				"reason":     "update config",
			})
			app.store.AddApproval(sessionID, model.ApprovalRequest{
				ID:           approvalID,
				RequestIDRaw: "raw-" + tc.name,
				HostID:       hostID,
				Type:         "remote_file_change",
				Status:       "accepted",
				ThreadID:     "thread-" + tc.name,
				TurnID:       "turn-" + tc.name,
				ItemID:       itemID,
				Reason:       "update config",
				Changes:      []model.FileChange{{Path: "/etc/app.conf", Kind: "update", Diff: "placeholder"}},
				RequestedAt:  now,
			})
			app.store.UpsertCard(sessionID, model.Card{
				ID:        itemID,
				Type:      "FileChangeApprovalCard",
				Status:    "pending",
				CreatedAt: now,
				UpdatedAt: now,
			})

			stream := &fileChangeAgentStream{
				onSend: func(msg *agentrpc.Envelope) error {
					if msg.FileWriteRequest != nil {
						app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
							RequestID: msg.FileWriteRequest.RequestID,
							Path:      msg.FileWriteRequest.Path,
							WriteMode: msg.FileWriteRequest.WriteMode,
							Message:   tc.message,
						})
					}
					return nil
				},
			}
			app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

			approval, ok := app.store.Approval(sessionID, approvalID)
			if !ok {
				t.Fatalf("expected approval to exist")
			}
			app.executeApprovedRemoteFileChange(sessionID, approval)

			card := app.cardByID(sessionID, itemID)
			if card == nil {
				t.Fatalf("expected card to exist")
			}
			if card.Status != "failed" {
				t.Fatalf("expected failed card, got %q", card.Status)
			}
			joined := strings.ToLower(card.Text)
			for _, part := range tc.wantParts {
				if !strings.Contains(joined, strings.ToLower(part)) {
					t.Fatalf("expected card text to contain %q, got %q", part, card.Text)
				}
			}
		})
	}
}

func TestHandleRemoteFileChangeApprovalAcceptIsIdempotent(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-file-change-idempotent"
	hostID := "linux-01"
	approvalID := "approval-idempotent"
	itemID := "item-idempotent"
	now := model.NowString()

	var mu sync.Mutex
	writeCount := 0
	firstWriteSeen := make(chan struct{}, 1)
	releaseFirstWrite := make(chan struct{})
	responded := make(chan struct{}, 1)

	app.codexRespondFunc = func(_ context.Context, _ string, _ any) error {
		select {
		case responded <- struct{}{}:
		default:
		}
		return nil
	}

	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})
	app.store.RememberItem(sessionID, itemID, map[string]any{
		"host":       hostID,
		"mode":       "file_change",
		"path":       "/etc/app.conf",
		"content":    "new-value\n",
		"write_mode": "overwrite",
		"writeMode":  "overwrite",
		"reason":     "update config",
	})
	app.store.AddApproval(sessionID, model.ApprovalRequest{
		ID:           approvalID,
		RequestIDRaw: "raw-idempotent",
		HostID:       hostID,
		Type:         "remote_file_change",
		Status:       "pending",
		ThreadID:     "thread-idempotent",
		TurnID:       "turn-idempotent",
		ItemID:       itemID,
		Reason:       "update config",
		Changes:      []model.FileChange{{Path: "/etc/app.conf", Kind: "update", Diff: "placeholder"}},
		RequestedAt:  now,
	})
	app.store.UpsertCard(sessionID, model.Card{
		ID:        itemID,
		Type:      "FileChangeApprovalCard",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})

	stream := &fileChangeAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			if msg.FileWriteRequest == nil {
				return nil
			}
			mu.Lock()
			writeCount++
			currentCount := writeCount
			mu.Unlock()

			if currentCount == 1 {
				select {
				case firstWriteSeen <- struct{}{}:
				default:
				}
			}
			<-releaseFirstWrite
			app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
				RequestID:  msg.FileWriteRequest.RequestID,
				Path:       msg.FileWriteRequest.Path,
				OldContent: "old-value\n",
				NewContent: msg.FileWriteRequest.Content,
				Created:    false,
				WriteMode:  msg.FileWriteRequest.WriteMode,
			})
			return nil
		},
	}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	firstReq := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approvalID+"/decision", strings.NewReader(`{"decision":"accept"}`))
	firstRec := httptest.NewRecorder()
	app.handleApprovalDecision(firstRec, firstReq, sessionID)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first accept to return 200, got %d", firstRec.Code)
	}

	select {
	case <-firstWriteSeen:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for first remote file write")
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approvalID+"/decision", strings.NewReader(`{"decision":"accept"}`))
	secondRec := httptest.NewRecorder()
	app.handleApprovalDecision(secondRec, secondReq, sessionID)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("expected duplicate accept to return 200, got %d", secondRec.Code)
	}

	close(releaseFirstWrite)
	select {
	case <-responded:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for codex response")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		card := app.cardByID(sessionID, itemID)
		if card != nil && card.Status == "completed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	gotWrites := writeCount
	mu.Unlock()
	if gotWrites != 1 {
		t.Fatalf("expected exactly one file write, got %d", gotWrites)
	}

	card := app.cardByID(sessionID, itemID)
	if card == nil {
		t.Fatalf("expected file change card to exist")
	}
	if card.Status != "completed" {
		t.Fatalf("expected completed file change card, got %q", card.Status)
	}
	approval, ok := app.store.Approval(sessionID, approvalID)
	if !ok {
		t.Fatalf("expected approval to exist")
	}
	if approval.Status != "accept" {
		t.Fatalf("expected approval to resolve as accept, got %q", approval.Status)
	}
}

func TestHandleRemoteFileChangeApprovalSequentialRequestsDoNotCrossPollute(t *testing.T) {
	app := New(config.Config{})
	sessionID := "sess-file-change-sequential"
	hostID := "linux-01"

	app.store.EnsureSession(sessionID)
	app.store.UpsertHost(model.Host{ID: hostID, Name: "linux-01", Kind: "agent", Status: "online", Executable: true})

	cases := []struct {
		name       string
		callID     string
		turnID     string
		itemID     string
		path       string
		oldContent string
		content    string
		writeMode  string
		reason     string
	}{
		{
			name:       "first",
			callID:     "call-first",
			turnID:     "turn-first",
			itemID:     dynamicToolCardID("call-first"),
			path:       "/etc/aiops-first.conf",
			oldContent: "first-old\n",
			content:    "first-new\n",
			writeMode:  "overwrite",
			reason:     "first change",
		},
		{
			name:       "second",
			callID:     "call-second",
			turnID:     "turn-second",
			itemID:     dynamicToolCardID("call-second"),
			path:       "/etc/aiops-second.conf",
			oldContent: "second-old\n",
			content:    "second-extra\n",
			writeMode:  "append",
			reason:     "second change",
		},
	}

	var mu sync.Mutex
	writeCount := 0
	stream := &fileChangeAgentStream{
		onSend: func(msg *agentrpc.Envelope) error {
			switch {
			case msg.FileReadRequest != nil:
				switch msg.FileReadRequest.Path {
				case cases[0].path:
					app.handleAgentFileReadResult(hostID, &agentrpc.FileReadResult{
						RequestID: msg.FileReadRequest.RequestID,
						Path:      msg.FileReadRequest.Path,
						Content:   cases[0].oldContent,
					})
				case cases[1].path:
					app.handleAgentFileReadResult(hostID, &agentrpc.FileReadResult{
						RequestID: msg.FileReadRequest.RequestID,
						Path:      msg.FileReadRequest.Path,
						Content:   cases[1].oldContent,
					})
				default:
					t.Fatalf("unexpected read path %q", msg.FileReadRequest.Path)
				}
			case msg.FileWriteRequest != nil:
				mu.Lock()
				writeCount++
				mu.Unlock()
				switch msg.FileWriteRequest.Path {
				case cases[0].path:
					app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
						RequestID:  msg.FileWriteRequest.RequestID,
						Path:       msg.FileWriteRequest.Path,
						OldContent: cases[0].oldContent,
						NewContent: msg.FileWriteRequest.Content,
						Created:    false,
						WriteMode:  msg.FileWriteRequest.WriteMode,
					})
				case cases[1].path:
					app.handleAgentFileWriteResult(hostID, &agentrpc.FileWriteResult{
						RequestID:  msg.FileWriteRequest.RequestID,
						Path:       msg.FileWriteRequest.Path,
						OldContent: cases[1].oldContent,
						NewContent: cases[1].oldContent + cases[1].content,
						Created:    false,
						WriteMode:  msg.FileWriteRequest.WriteMode,
					})
				default:
					t.Fatalf("unexpected write path %q", msg.FileWriteRequest.Path)
				}
			}
			return nil
		},
	}
	app.setAgentConnection(hostID, &agentConnection{hostID: hostID, stream: stream})

	for _, tc := range cases {
		app.requestRemoteFileChangeApproval(sessionID, hostID, "raw-"+tc.name, dynamicToolCallParams{
			ThreadID: "thread-sequential",
			TurnID:   tc.turnID,
			CallID:   tc.callID,
			Tool:     "execute_system_mutation",
		}, remoteFileChangeArgs{
			HostID:    hostID,
			Mode:      "file_change",
			Path:      tc.path,
			Content:   tc.content,
			WriteMode: tc.writeMode,
			Reason:    tc.reason,
		})

		approval := pendingRemoteFileChangeApproval(t, app, sessionID, tc.path)
		acceptApprovalDecision(t, app, sessionID, approval.ID)
		waitForCardStatus(t, app, sessionID, tc.itemID, "completed")

		card := app.cardByID(sessionID, tc.itemID)
		if card == nil {
			t.Fatalf("expected card for %s to exist", tc.path)
		}
		if card.Status != "completed" {
			t.Fatalf("expected completed card for %s, got %q", tc.path, card.Status)
		}
		if len(card.Changes) != 1 {
			t.Fatalf("expected one change for %s, got %#v", tc.path, card.Changes)
		}
		if card.Changes[0].Path != tc.path {
			t.Fatalf("expected card path %s, got %s", tc.path, card.Changes[0].Path)
		}
		if !strings.Contains(card.Text, tc.path) {
			t.Fatalf("expected card text to mention %s, got %q", tc.path, card.Text)
		}
		otherPath := cases[0].path
		if tc.path == cases[0].path {
			otherPath = cases[1].path
		}
		joined := card.Text + card.Summary + card.Changes[0].Diff
		if strings.Contains(joined, otherPath) {
			t.Fatalf("expected card for %s to stay isolated from %s, got %q", tc.path, otherPath, joined)
		}
	}

	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session to exist")
	}
	if session.Runtime.Activity.FilesChanged != 2 {
		t.Fatalf("expected filesChanged to be 2, got %d", session.Runtime.Activity.FilesChanged)
	}
	if session.Runtime.Activity.CurrentChangingFile != "" {
		t.Fatalf("expected currentChangingFile to be cleared, got %q", session.Runtime.Activity.CurrentChangingFile)
	}

	mu.Lock()
	gotWrites := writeCount
	mu.Unlock()
	if gotWrites != 2 {
		t.Fatalf("expected exactly two writes, got %d", gotWrites)
	}
}

func pendingRemoteFileChangeApproval(t *testing.T, app *App, sessionID, path string) model.ApprovalRequest {
	t.Helper()
	session := app.store.Session(sessionID)
	if session == nil {
		t.Fatalf("expected session %s to exist", sessionID)
	}
	for _, approval := range session.Approvals {
		if approval.Type != "remote_file_change" || approval.Status != "pending" {
			continue
		}
		for _, change := range approval.Changes {
			if change.Path == path {
				return approval
			}
		}
	}
	t.Fatalf("expected pending remote file change approval for %s", path)
	return model.ApprovalRequest{}
}

func acceptApprovalDecision(t *testing.T, app *App, sessionID, approvalID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+approvalID+"/decision", strings.NewReader(`{"decision":"accept"}`))
	rec := httptest.NewRecorder()
	app.handleApprovalDecision(rec, req, sessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected approval accept to return 200, got %d", rec.Code)
	}
}

func waitForCardStatus(t *testing.T, app *App, sessionID, cardID, wantStatus string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		card := app.cardByID(sessionID, cardID)
		if card != nil && card.Status == wantStatus {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for card %s to reach status %s", cardID, wantStatus)
}
