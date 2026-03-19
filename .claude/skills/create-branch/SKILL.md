---
name: create-branch
description: Create well-structured, consistently-named git branches following team naming conventions
user-invocable: true
---

# Create Branch

Create well-structured, consistently-named git branches following the team's naming conventions.

## Usage

```
/create-branch [work description]
```

Examples:
- `/create-branch add tenant connection limits` - Creates `feat/tenant-connection-limits`
- `/create-branch fix kafka consumer offset` - Creates `bugfix/kafka-consumer-offset`
- `/create-branch` - Will ask for a work description

## Instructions

1. **Determine work description**:
   - If provided as argument (`{{args}}`), use that description
   - If no description is provided, ask the user what they're working on
   - If the description is ambiguous, ask clarifying questions

2. **Run safety checks** by running these commands in parallel:
   - `git status` - Check for uncommitted changes
   - `git branch --show-current` - Get current branch name
   - If there are uncommitted changes, warn the user and offer options:
     - Stash changes
     - Commit changes first
     - Proceed anyway (if they confirm)

3. **Determine the branch prefix**:
   - `feat/` - New features, enhancements, or additions to functionality
   - `bugfix/` - Fixes for existing bugs or issues in development/staging
   - `hotfix/` - Critical, urgent fixes for production issues
   - `refactor/` - Code restructuring, optimization, or cleanup without functional changes
   - `docs/` - Documentation updates, README changes, or comment improvements
   - If work spans multiple categories, ask the user which aspect is primary

4. **Generate the branch name**:
   - Use kebab-case (lowercase with hyphens only)
   - 3-5 words maximum after the prefix
   - Capture the essence of the work clearly
   - No special characters except hyphens
   - Avoid redundant words like "add", "update", or "fix" since the prefix indicates action
   - Examples: `feat/tenant-connection-limits`, `bugfix/kafka-consumer-offset`, `hotfix/cloud-nat-exhaustion`, `refactor/collector-removal`, `docs/deployment-runbook`

5. **Present for confirmation**:
   - Show the chosen prefix with brief justification
   - Show the complete proposed branch name
   - Request explicit confirmation before proceeding

6. **Create the branch**:
   ```bash
   git checkout -b <prefix/branch-name>
   ```

7. **Verify the branch** by running `git branch --show-current` to confirm successful creation.

## Notes

- Always show the user the proposed branch name before creating it
- If the user requests a specific branch name that doesn't follow conventions, suggest the conventional alternative but defer to their preference
- For very long descriptions, extract the key 3-5 words that best represent the work
- Every branch name should immediately communicate its purpose to anyone on the team
