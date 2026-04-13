package mcphost

import (
	"testing"
)

func TestDeferredServer_NewDeferredServer(t *testing.T) {
	config := ServerConfig{
		Name:      "test-server",
		Transport: TransportSTDIO,
		Command:   "echo",
	}

	ds := NewDeferredServer(config)
	if ds == nil {
		t.Fatal("NewDeferredServer returned nil")
	}
	if ds.IsLoaded() {
		t.Error("new deferred server should not be loaded")
	}
	if ds.Config().Name != "test-server" {
		t.Errorf("expected test-server, got %s", ds.Config().Name)
	}
}

func TestDeferredServer_Tools_BeforeLoad(t *testing.T) {
	ds := NewDeferredServer(ServerConfig{Name: "test"})
	tools := ds.Tools()
	if tools != nil {
		t.Errorf("expected nil tools before load, got %v", tools)
	}
}

func TestDeferredServer_Close_BeforeLoad(t *testing.T) {
	ds := NewDeferredServer(ServerConfig{Name: "test"})
	err := ds.Close()
	if err != nil {
		t.Errorf("Close before load should not error: %v", err)
	}
}

func TestDeferredServer_IsLoaded(t *testing.T) {
	ds := NewDeferredServer(ServerConfig{Name: "test"})
	if ds.IsLoaded() {
		t.Error("should not be loaded initially")
	}
}
