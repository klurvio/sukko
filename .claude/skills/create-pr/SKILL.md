---
name: create-pr
description: Create a pull request to main branch with auto-generated description from GitHub issue
user-invocable: true
---

# Create Pull Request

Create a pull request to the `main` branch with an auto-generated description based on branch changes and GitHub issue context.

## Usage

```
/create-pr [github-issue-number]
```

## Instructions

1. **Gather git context** by running these commands in parallel:
   - `git branch --show-current` - Get current branch name
   - `git log main..HEAD --oneline` - Get commits on this branch
   - `git diff main...HEAD --stat` - Get summary of file changes
   - `git diff main...HEAD` - Get full diff (for understanding changes)

2. **Extract GitHub issue number**:
   - If provided as argument (`{{args}}`), use that issue number
   - Otherwise, extract from commit messages: `type[#issue]: description` or branch name patterns like `feat/123-description`
   - If no issue number found, ask the user to provide one

3. **Fetch GitHub issue context** using gh CLI:
   - Use `gh issue view <number> --json title,body,labels,state` to get:
     - Issue title and description
     - Labels and state
     - Acceptance criteria or requirements
   - Use `gh issue view <number> --comments` to get additional context from discussions
   - This provides the "why" behind the changes

4. **Analyze all context** to understand:
   - What feature/fix/refactor this PR implements (from GitHub issue)
   - The original requirements and acceptance criteria (from issue)
   - Which files and modules are affected (from git diff)
   - The scope and impact of changes

5. **Generate the PR description**:
   - **Summary**: 1-2 sentences explaining what this PR does and why (informed by GitHub issue)
   - **Changes**: Bullet points for each logical change, aligned with issue requirements
   - **Issue**: Reference to the GitHub issue using `Closes #NUMBER` format
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

   Closes #ISSUE_NUMBER

   ## Testing
   [verification steps]

   ## Deploy Notes
   [deployment instructions]
   EOF
   )"
   ```

   - Title format: `type[#issue]: short description`
   - Types: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`
   - Use the GitHub issue title to inform the PR title description

7. **Return the PR URL** to the user after creation.

## Notes

- GitHub issue context is essential — it provides the "why" and acceptance criteria
- If no GitHub issue number is found, warn the user and ask if they want to proceed without it
- If the branch has no commits ahead of main, inform the user
- If there are uncommitted changes, warn the user before creating the PR
- Keep the summary concise but informative
- Use `Closes #NUMBER` in the PR body to auto-close the issue on merge
