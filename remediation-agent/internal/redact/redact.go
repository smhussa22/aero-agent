// Package redact strips secrets from text before it is sent to an AI provider.
package redact

import "regexp"

type rule struct {
	re   *regexp.Regexp
	repl string
}

// shortcut: pattern-based, so unusual secret formats slip through; extend as new formats show up
var rules = []rule{
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), "[REDACTED PRIVATE KEY]"},
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{10,}`), "[REDACTED]"},
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`), "[REDACTED]"},
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "[REDACTED]"},
	{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "[REDACTED JWT]"},
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{12,}`), "${1}[REDACTED]"},
	{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/\s:@]+:)[^@\s]+(@)`), "${1}[REDACTED]${2}"},
	{regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|authorization)\s*[=:]\s*)\S+`), "${1}[REDACTED]"},
}

// Redact replaces anything that looks like a credential with a placeholder.
func Redact(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
