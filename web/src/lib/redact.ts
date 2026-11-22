// Strips credential-looking text before anything leaves the browser for an AI provider.
// Mirrors the remediation agent's Go redactor; extend both together.

const rules: [RegExp, string][] = [
  [/-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g, '[REDACTED PRIVATE KEY]'],
  [/sk-ant-[A-Za-z0-9_-]{10,}/g, '[REDACTED]'],
  [/sk-[A-Za-z0-9_-]{16,}/g, '[REDACTED]'],
  [/AKIA[0-9A-Z]{16}/g, '[REDACTED]'],
  [/gh[pousr]_[A-Za-z0-9]{20,}/g, '[REDACTED]'],
  [/eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/g, '[REDACTED JWT]'],
  [/(bearer\s+)[A-Za-z0-9._~+/=-]{12,}/gi, '$1[REDACTED]'],
  [/([a-z][a-z0-9+.-]*:\/\/[^/\s:@]+:)[^@\s]+(@)/gi, '$1[REDACTED]$2'],
  [/((?:password|passwd|pwd|secret|token|api[_-]?key|authorization)\s*[=:]\s*)\S+/gi, '$1[REDACTED]'],
]

export function redact(s: string): string {
  return rules.reduce((out, [re, repl]) => out.replace(re, repl), s)
}

// Fences untrusted text so it cannot close the fence early or smuggle control characters.
export function fence(label: string, text: string, max = 6000): string {
  const clean = text
    .replace(/<\s*\/?\s*untrusted_data\s*>/gi, '[tag removed]')
    // eslint-disable-next-line no-control-regex
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '')
  const clipped = clean.length > max ? clean.slice(0, max) + '\n... (truncated)' : clean
  return `## ${label}\n<untrusted_data>\n${clipped}\n</untrusted_data>`
}
