// Package orchestrator connects alerts, remediation agents and human approvals.
package orchestrator

import (
	"sync"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

// registry tracks which remediation agent is connected for each host.
type registry struct {
	mu     sync.Mutex
	agents map[string]chan *aerov1.OrchestratorMessage
}

func newRegistry() *registry {
	return &registry{agents: map[string]chan *aerov1.OrchestratorMessage{}}
}

// register makes out the live channel for host. A newer connection replaces an older one.
func (r *registry) register(host string, out chan *aerov1.OrchestratorMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[host] = out
}

// unregister removes host only if out is still its current connection, so a stale
// connection shutting down cannot evict its replacement.
func (r *registry) unregister(host string, out chan *aerov1.OrchestratorMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agents[host] == out {
		delete(r.agents, host)
	}
}

// send queues msg for host without blocking; false means no agent is connected or its queue is full.
func (r *registry) send(host string, msg *aerov1.OrchestratorMessage) bool {
	r.mu.Lock()
	out, ok := r.agents[host]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case out <- msg:
		return true
	default:
		return false
	}
}

func (r *registry) connected(host string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.agents[host]
	return ok
}
