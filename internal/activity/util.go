// Package activity contains Temporal activity implementations.
package activity

import (
	"strings"
)

// ShellQuote escapes a string for safe use in shell commands.
// Uses single quotes and escapes any embedded single quotes.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
