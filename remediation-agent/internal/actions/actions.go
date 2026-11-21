// Package actions is the allowlist of things the executor may ever run. The AI can only
// name an action and fill its parameters; every proposal is checked here first.
package actions

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Kind int

const (
	String Kind = iota
	Int
)

// Param describes one allowed parameter. Strings must match Pattern; ints must fall in [Min, Max].
type Param struct {
	Kind     Kind
	Pattern  *regexp.Regexp
	Min, Max int64
	Required bool
}

type Action struct {
	ID          string
	Description string
	Dangerous   bool // always needs a human approval, whatever the permission mode
	Params      map[string]Param
}

// Catalog maps action id to its definition.
type Catalog map[string]Action

// Default is the starting allowlist. Service names are only shape-checked here; restricting them
// to a user-configured list belongs to the executor.
func Default() Catalog {
	svc := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9@._-]{0,63}$`)
	sig := regexp.MustCompile(`^(TERM|KILL)$`)
	list := []Action{
		{ID: "restart_service", Description: "Restart a systemd service.",
			Params: map[string]Param{"service": {Kind: String, Pattern: svc, Required: true}}},
		{ID: "kill_process", Description: "Send TERM (default) or KILL to one process by pid.", Dangerous: true,
			Params: map[string]Param{
				"pid":    {Kind: Int, Min: 2, Max: 4194304, Required: true},
				"signal": {Kind: String, Pattern: sig},
			}},
		{ID: "vacuum_journal", Description: "Delete systemd journal entries older than N days to free disk.",
			Params: map[string]Param{"days": {Kind: Int, Min: 1, Max: 30, Required: true}}},
	}
	c := Catalog{}
	for _, a := range list {
		c[a.ID] = a
	}
	return c
}

// Proposal is one action the AI suggests. Dangerous is set from the catalog, never from the AI.
type Proposal struct {
	ActionID  string
	Params    map[string]string
	Rationale string
	Dangerous bool
}

const maxRationale = 500

// RawProposal is the untrusted JSON shape produced by the model.
type RawProposal struct {
	ActionID  string         `json:"action_id"`
	Params    map[string]any `json:"params"`
	Rationale string         `json:"rationale"`
}

func stringify(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case float64:
		if t != math.Trunc(t) || math.Abs(t) > 1e15 {
			return "", fmt.Errorf("not an integer: %v", t)
		}
		return strconv.FormatInt(int64(t), 10), nil
	case json.Number:
		return t.String(), nil
	}
	return "", fmt.Errorf("unsupported value type %T", v)
}

// Validate returns a normalized proposal or an error saying why it was refused.
func (c Catalog) Validate(r RawProposal) (Proposal, error) {
	a, ok := c[r.ActionID]
	if !ok {
		return Proposal{}, fmt.Errorf("action %q is not on the allowlist", r.ActionID)
	}

	params := map[string]string{}
	for name, raw := range r.Params {
		spec, ok := a.Params[name]
		if !ok {
			return Proposal{}, fmt.Errorf("unknown parameter %q", name)
		}
		val, err := stringify(raw)
		if err != nil {
			return Proposal{}, fmt.Errorf("parameter %q: %w", name, err)
		}
		switch spec.Kind {
		case String:
			if spec.Pattern == nil || !spec.Pattern.MatchString(val) {
				return Proposal{}, fmt.Errorf("parameter %q has a disallowed value", name)
			}
		case Int:
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < spec.Min || n > spec.Max {
				return Proposal{}, fmt.Errorf("parameter %q must be an integer in [%d, %d]", name, spec.Min, spec.Max)
			}
		}
		params[name] = val
	}
	for name, spec := range a.Params {
		if _, ok := params[name]; spec.Required && !ok {
			return Proposal{}, fmt.Errorf("missing required parameter %q", name)
		}
	}

	why := strings.TrimSpace(r.Rationale)
	if len(why) > maxRationale {
		why = strings.ToValidUTF8(why[:maxRationale], "")
	}
	return Proposal{ActionID: a.ID, Params: params, Rationale: why, Dangerous: a.Dangerous}, nil
}
