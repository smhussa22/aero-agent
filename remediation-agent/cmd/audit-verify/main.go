// Command audit-verify checks that an agent audit log has not been edited, reordered or truncated
// in the middle: audit-verify /var/lib/aero-remediation/audit.log
package main

import (
	"fmt"
	"os"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/audit"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: audit-verify <audit.log>")
		os.Exit(2)
	}
	if err := audit.Verify(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("ok: audit log chain is intact")
}
