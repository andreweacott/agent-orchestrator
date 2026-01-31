// Package activity contains Temporal activity implementations.
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := shellQuote(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsValidEnvKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		// Valid keys
		{name: "simple uppercase", key: "FOO", expected: true},
		{name: "simple lowercase", key: "foo", expected: true},
		{name: "mixed case", key: "FooBar", expected: true},
		{name: "with underscore", key: "FOO_BAR", expected: true},
		{name: "starting with underscore", key: "_FOO", expected: true},
		{name: "with numbers", key: "FOO123", expected: true},
		{name: "underscore and numbers", key: "FOO_123_BAR", expected: true},
		{name: "single letter", key: "A", expected: true},
		{name: "single underscore", key: "_", expected: true},

		// Invalid keys
		{name: "empty string", key: "", expected: false},
		{name: "starts with number", key: "1FOO", expected: false},
		{name: "contains hyphen", key: "FOO-BAR", expected: false},
		{name: "contains space", key: "FOO BAR", expected: false},
		{name: "contains dot", key: "FOO.BAR", expected: false},
		{name: "contains equals", key: "FOO=BAR", expected: false},
		{name: "contains special char", key: "FOO$BAR", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidEnvKey(tc.key)
			assert.Equal(t, tc.expected, result, "isValidEnvKey(%q)", tc.key)
		})
	}
}

func TestBuildDockerRunCommand(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		args     []string
		env      map[string]string
		contains []string // substrings that must be present
	}{
		{
			name:  "basic image only",
			image: "alpine:latest",
			args:  nil,
			env:   nil,
			contains: []string{
				"docker run --rm",
				"-v /workspace:/workspace",
				"-w /workspace",
				"'alpine:latest'",
			},
		},
		{
			name:  "image with args",
			image: "myimage:v1",
			args:  []string{"--fix", "--verbose"},
			env:   nil,
			contains: []string{
				"'myimage:v1'",
				"'--fix'",
				"'--verbose'",
			},
		},
		{
			name:  "image with env vars",
			image: "transformer:latest",
			args:  nil,
			env:   map[string]string{"FOO": "bar", "BAZ": "qux"},
			contains: []string{
				"-e BAZ='qux'",
				"-e FOO='bar'",
			},
		},
		{
			name:  "env var with special chars",
			image: "myimage",
			args:  nil,
			env:   map[string]string{"PASSWORD": "p@ss'word"},
			contains: []string{
				"-e PASSWORD='p@ss'\"'\"'word'",
			},
		},
		{
			name:  "full command",
			image: "openrewrite/rewrite:latest",
			args:  []string{"rewrite:run", "-Drewrite.activeRecipes=org.foo.Bar"},
			env:   map[string]string{"MAVEN_OPTS": "-Xmx2g"},
			contains: []string{
				"docker run --rm",
				"-v /workspace:/workspace",
				"-w /workspace",
				"-e MAVEN_OPTS='-Xmx2g'",
				"'openrewrite/rewrite:latest'",
				"'rewrite:run'",
				"'-Drewrite.activeRecipes=org.foo.Bar'",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := buildDockerRunCommand(tc.image, tc.args, tc.env)
			for _, substr := range tc.contains {
				assert.Contains(t, result, substr)
			}
		})
	}
}

func TestBuildDockerRunCommandSecurityHardening(t *testing.T) {
	// Verify all security hardening options are present
	result := buildDockerRunCommand("alpine:latest", nil, nil)

	securityOptions := []string{
		"--network none",              // Network isolation
		"--cap-drop=ALL",              // Drop all capabilities
		"--read-only",                 // Read-only root filesystem
		"--security-opt=no-new-privileges:true", // Prevent privilege escalation
		"--tmpfs /tmp:rw,noexec,nosuid,size=512m", // Writable /tmp with noexec
	}

	for _, opt := range securityOptions {
		assert.Contains(t, result, opt, "Security option should be present: %s", opt)
	}
}

func TestBuildDockerRunCommandEnvSorting(t *testing.T) {
	// Verify environment variables are sorted for deterministic output
	env := map[string]string{
		"ZEBRA": "z",
		"APPLE": "a",
		"MANGO": "m",
	}

	result := buildDockerRunCommand("image", nil, env)

	// Find positions of each env var
	applePos := indexOf(result, "-e APPLE=")
	mangoPos := indexOf(result, "-e MANGO=")
	zebraPos := indexOf(result, "-e ZEBRA=")

	assert.True(t, applePos < mangoPos, "APPLE should come before MANGO")
	assert.True(t, mangoPos < zebraPos, "MANGO should come before ZEBRA")
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
