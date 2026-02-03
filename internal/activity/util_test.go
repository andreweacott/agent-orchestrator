package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "simple",
			expected: "'simple'",
		},
		{
			name:     "string with spaces",
			input:    "with spaces",
			expected: "'with spaces'",
		},
		{
			name:     "string with single quote",
			input:    "it's",
			expected: "'it'\"'\"'s'",
		},
		{
			name:     "string with multiple quotes",
			input:    "it's a 'test'",
			expected: "'it'\"'\"'s a '\"'\"'test'\"'\"''",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "string with special chars",
			input:    "hello$world",
			expected: "'hello$world'",
		},
		{
			name:     "string with newline",
			input:    "hello\nworld",
			expected: "'hello\nworld'",
		},
		{
			name:     "string with backticks",
			input:    "echo `date`",
			expected: "'echo `date`'",
		},
		{
			name:     "simple filename",
			input:    "file.txt",
			expected: "'file.txt'",
		},
		{
			name:     "filename with spaces",
			input:    "my file.txt",
			expected: "'my file.txt'",
		},
		{
			name:     "filename with single quote",
			input:    "file's.txt",
			expected: "'file'\"'\"'s.txt'",
		},
		{
			name:     "filename with shell metacharacters",
			input:    "file; rm -rf /",
			expected: "'file; rm -rf /'",
		},
		{
			name:     "filename with command substitution",
			input:    "$(whoami).txt",
			expected: "'$(whoami).txt'",
		},
		{
			name:     "filename with backticks",
			input:    "`id`.txt",
			expected: "'`id`.txt'",
		},
		{
			name:     "filename with double quotes",
			input:    `file"name.txt`,
			expected: `'file"name.txt'`,
		},
		{
			name:     "filename with newline",
			input:    "file\nname.txt",
			expected: "'file\nname.txt'",
		},
		{
			name:     "path with special chars",
			input:    "../../../etc/passwd",
			expected: "'../../../etc/passwd'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShellQuote(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
