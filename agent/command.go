package agent

import (
	"context"
	"strings"

	"github.com/entreya/nativestudio/db"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

// registerCommandTool adds run_command — the agent's only path to real shell
// commands (package installs, scaffolding tools, build steps), matching
// what a developer would type into their own terminal instead of
// hand-authoring framework files it has no business writing itself.
//
// Unlike the file tools, a command's cwd is the only thing actually
// contained to the workspace: once approved, the command text runs as a
// real shell command and can do anything a shell command can. The safety
// model here is the same one a human already trusts when they type a
// command into their own terminal — nothing runs until the user reads the
// exact command and explicitly approves it (Dangerous tier, staged exactly
// like create_file/delete_file, executed only by the approval handler).
func registerCommandTool(r *Registry) {
	r.Register(&Tool{
		Name: "run_command",
		Description: "Stage a shell command to run in the workspace (or a subdirectory of it) — for package installs, project scaffolding " +
			"(composer create-project, npm install, git init, etc.), and build/test commands. The command does not run until the user reviews " +
			"and approves it. Prefer this over hand-writing framework/vendor files yourself.",
		Parameters: objectParameters(map[string]any{
			"command": map[string]any{"type": "string", "description": "The exact shell command to run, e.g. \"composer create-project yiisoft/yii2-app-basic .\"."},
			"cwd":     map[string]any{"type": "string", "description": "Directory to run it in, relative to the workspace root. Defaults to the workspace root."},
		}, "command"),
		Safety:  Dangerous,
		Execute: executeRunCommand,
	})
}

func executeRunCommand(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	command, _ := input["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return ToolResult{OK: false, Error: "command is required"}, nil
	}
	cwdInput, _ := input["cwd"].(string)
	return stageCommandForApproval(ctx, meta, command, cwdInput)
}

// stageCommandForApproval creates the CommandRun row that surfaces a
// command in the approval popup (CommandReview) and waits for a human to
// approve or reject it before CommandHandler.Approve actually runs it.
// Shared by run_command (everything routes through here) and run_terminal
// (only for the destructive commands it refuses to run instantly).
func stageCommandForApproval(ctx context.Context, meta ToolMeta, command, cwdInput string) (ToolResult, error) {
	if cwdInput == "" {
		cwdInput = "."
	}
	guard := workspacefs.Guard{}
	resolvedCwd, err := guard.ResolveDirectory(meta.WorkspaceRoot, cwdInput)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	relativeCwd, err := (workspacefs.Guard{Root: meta.WorkspaceRoot}).Relative(resolvedCwd)
	if err != nil {
		relativeCwd = cwdInput
	}

	if meta.DB == nil || meta.SessionID == "" || meta.RunID == "" {
		return ToolResult{OK: false, Error: "command_metadata_unavailable"}, nil
	}
	run := db.CommandRun{ID: db.NewID(), SessionID: meta.SessionID, AgentRunID: meta.RunID, Command: command, Cwd: relativeCwd}
	if err := meta.DB.CreateCommandRun(ctx, run); err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}

	return ToolResult{OK: true, Content: map[string]any{
		"staged": true, "run_id": run.ID, "command": command, "cwd": relativeCwd,
	}}, nil
}
