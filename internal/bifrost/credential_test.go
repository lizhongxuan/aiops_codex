package bifrost

import (
	"sync"
	"testing"
	"time"
)

// --- helpers ---

func newTestCreds() []*Credential {
	return []*Credential{
		{ID: "c1", Provider: "openai", APIKey: "sk-1", Status: "active"},
		{ID: "c2", Provider: "openai", APIKey: "sk-2", Status: "active"},
		{ID: "c3", Provider: "openai", APIKey: "sk-3", Status: "active"},
		{ID: "c4", Provider: "anthropic", APIKey: "ant-1", Status: "active"},
	}
}

// --- Select: basic ---

func TestSelect_ReturnsAvailableCredential(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())
	key, err := pool.Select("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk-1" {
		t.Errorf("got %q, want %q", key, "sk-1")
	}
}

func TestSelect_NoCredentialsForProvider(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())
	_, err := pool.Select("ollama")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// --- Round-robin ---

func TestSelect_RoundRobin(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())

	expected := []string{"sk-1", "sk-2", "sk-3", "sk-1", "sk-2"}
	for i, want := range expected {
		key, err := pool.Select("openai")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if key != want {
			t.Errorf("call %d: got %q, want %q", i, key, want)
		}
	}
}

func TestSelect_RoundRobinPerProvider(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())

	// openai should start at sk-1
	key, _ := pool.Select("openai")
	if key != "sk-1" {
		t.Errorf("openai: got %q, want %q", key, "sk-1")
	}

	// anthropic should independently start at ant-1
	key, _ = pool.Select("anthropic")
	if key != "ant-1" {
		t.Errorf("anthropic: got %q, want %q", key, "ant-1")
	}

	// openai should continue to sk-2
	key, _ = pool.Select("openai")
	if key != "sk-2" {
		t.Errorf("openai second call: got %q, want %q", key, "sk-2")
	}
}

// --- Skip exhausted ---

func TestSelect_SkipsExhausted(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())

	// Exhaust sk-1 with a long cooldown
	pool.MarkExhaustedWithCooldown("openai", "sk-1", 10*time.Minute)

	key, err := pool.Select("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk-2" {
		t.Errorf("got %q, want %q (should skip exhausted sk-1)", key, "sk-2")
	}
}

// --- Auto-recovery ---

func TestSelect_AutoRecoveryAfterCooldown(t *testing.T) {
	creds := []*Credential{
		{ID: "c1", Provider: "openai", APIKey: "sk-1", Status: "exhausted",
			ExhaustedUntil: time.Now().Add(-1 * time.Second)}, // cooldown expired
	}
	pool := NewCredentialPool(creds)

	key, err := pool.Select("openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sk-1" {
		t.Errorf("got %q, want %q (should auto-recover)", key, "sk-1")
	}
	if creds[0].Status != "active" {
		t.Errorf("status: got %q, want %q", creds[0].Status, "active")
	}
}

// --- All exhausted ---

func TestSelect_AllExhaustedReturnsError(t *testing.T) {
	creds := []*Credential{
		{ID: "c1", Provider: "openai", APIKey: "sk-1", Status: "active"},
		{ID: "c2", Provider: "openai", APIKey: "sk-2", Status: "active"},
	}
	pool := NewCredentialPool(creds)

	pool.MarkExhaustedWithCooldown("openai", "sk-1", 10*time.Minute)
	pool.MarkExhaustedWithCooldown("openai", "sk-2", 10*time.Minute)

	_, err := pool.Select("openai")
	if err == nil {
		t.Fatal("expected error when all credentials are exhausted")
	}
}

// --- MarkExhausted ---

func TestMarkExhausted_SetsDefaultCooldown(t *testing.T) {
	creds := []*Credential{
		{ID: "c1", Provider: "openai", APIKey: "sk-1", Status: "active"},
	}
	pool := NewCredentialPool(creds)

	before := time.Now()
	pool.MarkExhausted("openai", "sk-1")
	after := time.Now()

	c := creds[0]
	if c.Status != "exhausted" {
		t.Errorf("status: got %q, want %q", c.Status, "exhausted")
	}

	expectedMin := before.Add(DefaultCooldown)
	expectedMax := after.Add(DefaultCooldown)
	if c.ExhaustedUntil.Before(expectedMin) || c.ExhaustedUntil.After(expectedMax) {
		t.Errorf("ExhaustedUntil %v not in expected range [%v, %v]",
			c.ExhaustedUntil, expectedMin, expectedMax)
	}
}

// --- MarkExhaustedWithCooldown ---

func TestMarkExhaustedWithCooldown(t *testing.T) {
	creds := []*Credential{
		{ID: "c1", Provider: "openai", APIKey: "sk-1", Status: "active"},
	}
	pool := NewCredentialPool(creds)

	cooldown := 5 * time.Minute
	before := time.Now()
	pool.MarkExhaustedWithCooldown("openai", "sk-1", cooldown)
	after := time.Now()

	c := creds[0]
	if c.Status != "exhausted" {
		t.Errorf("status: got %q, want %q", c.Status, "exhausted")
	}

	expectedMin := before.Add(cooldown)
	expectedMax := after.Add(cooldown)
	if c.ExhaustedUntil.Before(expectedMin) || c.ExhaustedUntil.After(expectedMax) {
		t.Errorf("ExhaustedUntil %v not in expected range [%v, %v]",
			c.ExhaustedUntil, expectedMin, expectedMax)
	}
}

func TestMarkExhaustedWithCooldown_AuthError(t *testing.T) {
	creds := []*Credential{
		{ID: "c1", Provider: "openai", APIKey: "sk-bad", Status: "active"},
	}
	pool := NewCredentialPool(creds)

	before := time.Now()
	pool.MarkExhaustedWithCooldown("openai", "sk-bad", AuthErrorCooldown)

	c := creds[0]
	if c.Status != "exhausted" {
		t.Errorf("status: got %q, want %q", c.Status, "exhausted")
	}

	expectedMin := before.Add(AuthErrorCooldown)
	if c.ExhaustedUntil.Before(expectedMin) {
		t.Errorf("ExhaustedUntil %v should be at least %v for auth error cooldown",
			c.ExhaustedUntil, expectedMin)
	}
}

func TestMarkExhausted_NonexistentCredential(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())
	// Should not panic when marking a nonexistent credential.
	pool.MarkExhausted("openai", "sk-nonexistent")
}

// --- Concurrent access ---

func TestConcurrentAccess(t *testing.T) {
	pool := NewCredentialPool(newTestCreds())
	var wg sync.WaitGroup

	// Concurrent selects
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Select("openai")
		}()
	}

	// Concurrent marks
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			keys := []string{"sk-1", "sk-2", "sk-3"}
			pool.MarkExhausted("openai", keys[n%3])
		}(i)
	}

	// Concurrent selects on different provider
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Select("anthropic")
		}()
	}

	wg.Wait()
}
