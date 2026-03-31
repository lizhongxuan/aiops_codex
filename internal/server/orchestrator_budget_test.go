package server

import (
	"testing"
	"time"
)

func TestOrchestratorPermitRateLimitBlocksAfterThreadBurst(t *testing.T) {
	app := newOrchestratorTestApp(t)

	for i := 0; i < orchestratorThreadCreateRateLimit; i++ {
		if ok := app.tryReserveOrchestratorPermit("mission-1", "sess-"+string(rune('a'+i)), 32, 8, true); !ok {
			t.Fatalf("expected permit %d to succeed", i+1)
		}
	}

	if ok := app.tryReserveOrchestratorPermit("mission-1", "sess-z", 32, 8, true); ok {
		t.Fatalf("expected burst beyond thread limit to be rejected")
	}

	app.releaseOrchestratorPermit("sess-a")
	time.Sleep(orchestratorRateWindow + 50*time.Millisecond)
	if ok := app.tryReserveOrchestratorPermit("mission-1", "sess-z", 32, 8, true); !ok {
		t.Fatalf("expected permit to succeed after rate window elapsed")
	}
}
