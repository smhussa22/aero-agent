package orchestrator

import (
	"testing"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

func TestSendReachesOnlyTheRightHost(t *testing.T) {
	r := newRegistry()
	a, b := make(chan *aerov1.OrchestratorMessage, 1), make(chan *aerov1.OrchestratorMessage, 1)
	r.register("a", a)
	r.register("b", b)

	if !r.send("a", &aerov1.OrchestratorMessage{}) || len(a) != 1 || len(b) != 0 {
		t.Fatal("message must reach only host a")
	}
	if r.send("nobody", &aerov1.OrchestratorMessage{}) {
		t.Fatal("sending to an unconnected host must report failure")
	}
}

func TestFullQueueDoesNotBlock(t *testing.T) {
	r := newRegistry()
	r.register("a", make(chan *aerov1.OrchestratorMessage, 1))
	r.send("a", &aerov1.OrchestratorMessage{})
	if r.send("a", &aerov1.OrchestratorMessage{}) {
		t.Fatal("a full queue must drop, not block")
	}
}

func TestStaleConnectionCannotEvictReplacement(t *testing.T) {
	r := newRegistry()
	old, fresh := make(chan *aerov1.OrchestratorMessage, 1), make(chan *aerov1.OrchestratorMessage, 1)
	r.register("a", old)
	r.register("a", fresh) // the agent reconnected
	r.unregister("a", old) // the old connection finally notices it is dead

	if !r.connected("a") || !r.send("a", &aerov1.OrchestratorMessage{}) || len(fresh) != 1 {
		t.Fatal("the replacement connection was evicted")
	}
	r.unregister("a", fresh)
	if r.connected("a") {
		t.Fatal("unregister of the current connection must remove it")
	}
}
