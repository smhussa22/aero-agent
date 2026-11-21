package actions

import "testing"

func raw(id string, params map[string]any) RawProposal {
	return RawProposal{ActionID: id, Params: params, Rationale: "because"}
}

func TestValidAndDangerousComesFromCatalog(t *testing.T) {
	c := Default()

	p, err := c.Validate(raw("restart_service", map[string]any{"service": "nginx.service"}))
	if err != nil || p.Params["service"] != "nginx.service" || p.Dangerous {
		t.Fatalf("restart: %+v %v", p, err)
	}

	// the model sends pid as a JSON number; it must be accepted and normalized
	p, err = c.Validate(raw("kill_process", map[string]any{"pid": float64(4242)}))
	if err != nil || p.Params["pid"] != "4242" || !p.Dangerous {
		t.Fatalf("kill: %+v %v", p, err)
	}
}

func TestRejected(t *testing.T) {
	c := Default()
	for name, r := range map[string]RawProposal{
		"unknown action":     raw("run_shell", map[string]any{"cmd": "rm -rf /"}),
		"empty action":       raw("", nil),
		"shell injection":    raw("restart_service", map[string]any{"service": "nginx; rm -rf /"}),
		"command substitute": raw("restart_service", map[string]any{"service": "$(reboot)"}),
		"leading dash":       raw("restart_service", map[string]any{"service": "--help"}),
		"path traversal":     raw("restart_service", map[string]any{"service": "../../etc/passwd"}),
		"too long":           raw("restart_service", map[string]any{"service": string(make([]byte, 80))}),
		"unknown param":      raw("restart_service", map[string]any{"service": "nginx", "extra": "x"}),
		"missing required":   raw("restart_service", nil),
		"pid 1":              raw("kill_process", map[string]any{"pid": float64(1)}),
		"pid not integer":    raw("kill_process", map[string]any{"pid": 12.5}),
		"pid as text":        raw("kill_process", map[string]any{"pid": "12; ls"}),
		"bad signal":         raw("kill_process", map[string]any{"pid": float64(10), "signal": "STOP"}),
		"days out of range":  raw("vacuum_journal", map[string]any{"days": float64(0)}),
		"param wrong type":   raw("restart_service", map[string]any{"service": []any{"a"}}),
	} {
		if _, err := c.Validate(r); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRationaleIsCapped(t *testing.T) {
	r := raw("vacuum_journal", map[string]any{"days": float64(7)})
	r.Rationale = string(make([]byte, 5000))
	p, err := Default().Validate(r)
	if err != nil || len(p.Rationale) > maxRationale {
		t.Fatalf("rationale len=%d err=%v", len(p.Rationale), err)
	}
}
