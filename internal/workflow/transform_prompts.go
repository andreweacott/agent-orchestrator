// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// buildPrompt creates the prompt for Claude Code.
func buildPrompt(task model.Task) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))

	if task.Execution.Agentic != nil && task.Execution.Agentic.Prompt != "" {
		sb.WriteString(fmt.Sprintf("Instructions:\n%s\n\n", task.Execution.Agentic.Prompt))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("Related ticket: %s\n\n", *task.TicketURL))
	}

	sb.WriteString("Repositories to work on:\n")
	effectiveRepos := task.GetEffectiveRepositories()
	for _, repo := range effectiveRepos {
		repoPath := getRepoPath(task, repo)
		sb.WriteString(fmt.Sprintf("- %s (in %s)\n", repo.Name, repoPath))
	}

	sb.WriteString("\nPlease analyze the codebase and implement the necessary fix. ")
	sb.WriteString("Follow the existing code style and patterns. ")
	sb.WriteString("Make minimal, targeted changes to address the issue.")

	// Append verifier instructions if verifiers are defined
	verifiers := task.Execution.GetVerifiers()
	if len(verifiers) > 0 {
		sb.WriteString("\n\n## Verification\n\n")
		sb.WriteString("After making changes, verify your work by running these commands:\n\n")
		for _, v := range verifiers {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", v.Name, strings.Join(v.Command, " ")))
		}
		sb.WriteString("\nFix any errors before completing the task. All verifiers must pass.")
	}

	// Append report mode instructions if in report mode
	if task.GetMode() == model.TaskModeReport {
		sb.WriteString("\n\n## Output Requirements\n\n")
		if len(effectiveRepos) == 1 {
			repoPath := getRepoPath(task, effectiveRepos[0])
			sb.WriteString(fmt.Sprintf("Write your report to `%s/REPORT.md` with YAML frontmatter:\n\n", repoPath))
		} else {
			sb.WriteString("For each repository, write a report to the appropriate REPORT.md with YAML frontmatter:\n\n")
			for _, repo := range effectiveRepos {
				repoPath := getRepoPath(task, repo)
				sb.WriteString(fmt.Sprintf("- `%s/REPORT.md`\n", repoPath))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("```markdown\n---\nkey: value\nanother_key: value\n---\n\n# Report\n\nYour analysis and findings here.\n```\n\n")
		sb.WriteString("The frontmatter section (between `---` delimiters) should contain structured data.\n")
		sb.WriteString("The body section (after the closing `---`) should contain your detailed analysis.\n")

		if hasSchema(task) {
			sb.WriteString("\nThe frontmatter must conform to this JSON Schema:\n\n```json\n")
			sb.WriteString(string(task.Execution.Agentic.Output.Schema))
			sb.WriteString("\n```\n")
		}
	}

	return sb.String()
}

// substitutePromptTemplate substitutes {{.Name}} and {{.Context}} variables in the prompt.
func substitutePromptTemplate(prompt string, target model.ForEachTarget) (string, error) {
	tmpl, err := template.New("prompt").Parse(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, target); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildPromptForTarget creates the prompt for a specific forEach target.
// It substitutes template variables and appends report output instructions.
func buildPromptForTarget(task model.Task, target model.ForEachTarget, reportPath string) (string, error) {
	if task.Execution.Agentic == nil || task.Execution.Agentic.Prompt == "" {
		return "", fmt.Errorf("agentic execution requires prompt to be set")
	}

	// Substitute template variables in the original prompt
	substitutedPrompt, err := substitutePromptTemplate(task.Execution.Agentic.Prompt, target)
	if err != nil {
		return "", err
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("Target: %s\n\n", target.Name))
	sb.WriteString(fmt.Sprintf("Instructions:\n%s\n\n", substitutedPrompt))

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("Related ticket: %s\n\n", *task.TicketURL))
	}

	sb.WriteString("Repositories to work on:\n")
	effectiveRepos := task.GetEffectiveRepositories()
	for _, repo := range effectiveRepos {
		repoPath := getRepoPath(task, repo)
		sb.WriteString(fmt.Sprintf("- %s (in %s)\n", repo.Name, repoPath))
	}

	sb.WriteString("\nPlease analyze the codebase and complete the task. ")
	sb.WriteString("Follow the existing code style and patterns. ")
	sb.WriteString("Focus specifically on the target described above.")

	// Append verifier instructions if verifiers are defined
	verifiers := task.Execution.GetVerifiers()
	if len(verifiers) > 0 {
		sb.WriteString("\n\n## Verification\n\n")
		sb.WriteString("After making changes, verify your work by running these commands:\n\n")
		for _, v := range verifiers {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", v.Name, strings.Join(v.Command, " ")))
		}
		sb.WriteString("\nFix any errors before completing the task. All verifiers must pass.")
	}

	// Append report mode instructions with target-specific path
	sb.WriteString("\n\n## Output Requirements\n\n")
	sb.WriteString(fmt.Sprintf("Write your report to `%s` with YAML frontmatter:\n\n", reportPath))
	sb.WriteString("```markdown\n---\nkey: value\nanother_key: value\n---\n\n# Report\n\nYour analysis and findings here.\n```\n\n")
	sb.WriteString("The frontmatter section (between `---` delimiters) should contain structured data.\n")
	sb.WriteString("The body section (after the closing `---`) should contain your detailed analysis.\n")

	if hasSchema(task) {
		sb.WriteString("\nThe frontmatter must conform to this JSON Schema:\n\n```json\n")
		sb.WriteString(string(task.Execution.Agentic.Output.Schema))
		sb.WriteString("\n```\n")
	}

	return sb.String(), nil
}

// buildDiffSummary creates a human-readable summary of diffs for notifications.
func buildDiffSummary(diffs []model.DiffOutput) string {
	if len(diffs) == 0 {
		return "No changes detected."
	}

	var sb strings.Builder
	sb.WriteString("**Diff summary:**\n")

	for _, diff := range diffs {
		if len(diff.Files) == 0 {
			sb.WriteString(fmt.Sprintf("- **%s**: no changes\n", diff.Repository))
			continue
		}

		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", diff.Repository, diff.Summary))
		for _, f := range diff.Files {
			sb.WriteString(fmt.Sprintf("  - %s (%s, +%d/-%d)\n", f.Path, f.Status, f.Additions, f.Deletions))
		}
	}

	return sb.String()
}
