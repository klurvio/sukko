---
name: create-pr
description: Create a pull request to main branch with auto-generated description from ClickUp task
user-invocable: true
---

# Create Pull Request

Create a pull request to the `main` branch with an auto-generated description based on branch changes and ClickUp task context.

## Usage

```
/create-pr [clickup-task-id]
```

## Instructions

1. **Gather git context** by running these commands in parallel:
   - `git branch --show-current` - Get current branch name
   - `git log main..HEAD --oneline` - Get commits on this branch
   - `git diff main...HEAD --stat` - Get summary of file changes
   - `git diff main...HEAD` - Get full diff (for understanding changes)

2. **Extract ClickUp task ID**:
   - If provided as argument (`{{args}}`), use that task ID
   - Otherwise, extract from commit messages: `type[task-id]: description`
   - Task IDs are alphanumeric strings that may include hyphens/underscores
   - If no task ID found, ask the user to provide one

3. **Fetch ClickUp task context** using MCP tools:
   - Workspace ID: `9003098945`
   - Use `clickup_get_task` with the task ID and workspace_id to get:
     - Task name and description
     - Status and priority
     - Acceptance criteria or requirements
   - Use `clickup_get_task_comments` to get additional context from task discussions
   - This provides the "why" behind the changes

4. **Analyze all context** to understand:
   - What feature/fix/refactor this PR implements (from ClickUp task)
   - The original requirements and acceptance criteria (from ClickUp)
   - Which files and modules are affected (from git diff)
   - The scope and impact of changes

5. **Generate the PR description**:
   - **Summary**: 1-2 sentences explaining what this PR does and why (informed by ClickUp task)
   - **Changes**: Bullet points for each logical change, aligned with task requirements
   - **ClickUp Task**: Link to the ClickUp task using format `https://app.clickup.com/t/TASK_ID`
   - **Testing**: How changes were tested / verification steps
   - **Deploy Notes**: Any special deployment steps (image rebuild, config-only, Terraform apply)

6. **Create the PR** using:
   ```bash
   gh pr create --base main --title "TITLE" --body "$(cat <<'EOF'
   ## Summary
   [1-2 sentences]

   ## Changes
   - [change 1]
   - [change 2]

   ## ClickUp Task
   https://app.clickup.com/t/TASK_ID

   ## Testing
   [verification steps]

   ## Deploy Notes
   [deployment instructions]
   EOF
   )"
   ```

   - Title format: `type[task-id]: short description`
   - Types: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`
   - Use the ClickUp task name to inform the title description

7. **Return the PR URL** to the user after creation.

## Notes

- ClickUp task context is essential — it provides the "why" and acceptance criteria
- If no ClickUp task ID is found, warn the user and ask if they want to proceed without it
- If the branch has no commits ahead of main, inform the user
- If there are uncommitted changes, warn the user before creating the PR
- Keep the summary concise but informative
- ClickUp workspace ID: `9003098945`
- ClickUp task link format: `https://app.clickup.com/t/TASK_ID`
