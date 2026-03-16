---
name: commit
description: Create a commit following Conventional Commits format
user-invocable: true
---

# Create Commit

Create a commit following Conventional Commits format.

## Usage

```
/commit
```

## Format

```
type: subject (min 4 chars)

optional body with more details
```

## Instructions

1. **Check for and remove binary files from remote**:
   ```bash
   git ls-files | xargs -I {} sh -c 'file --mime "{}" 2>/dev/null | grep -q "charset=binary" && echo "{}"' | xargs -r git rm --cached
   ```

2. **Stage all non-binary changes**:
   - Stage modified/deleted tracked files: `git add -u`
   - Stage new untracked files (excluding binaries):
     ```bash
     git ls-files --others --exclude-standard | while IFS= read -r f; do
       mime=$(file --mime "$f" 2>/dev/null)
       echo "$mime" | grep -qv "charset=binary" && git add "$f"
     done
     ```

3. **Gather git context** by running these commands in parallel:
   - `git diff --cached --stat` - Summary of staged changes
   - `git diff --cached` - Full diff of staged changes
   - `git branch --show-current` - Get current branch name

4. **If nothing is staged**, inform the user there's nothing to commit

5. **Analyze the staged changes** to understand:
   - What type of change this is (feat, fix, refactor, etc.)
   - What the change does in plain language
   - Whether a detailed body is needed

6. **Determine the commit type**:
   - `feat` - A new feature
   - `fix` - A bug fix
   - `docs` - Documentation only changes
   - `style` - Code style changes (formatting, semicolons, etc.)
   - `refactor` - Code change that neither fixes a bug nor adds a feature
   - `perf` - Performance improvements
   - `test` - Adding or updating tests
   - `build` - Changes to build system or dependencies
   - `ci` - Changes to CI configuration files and scripts
   - `chore` - Other changes that don't modify src or test files
   - `revert` - Reverts a previous commit

7. **Generate the commit message**:
   - First line: `type: subject` (subject must be at least 4 characters)
   - Use imperative mood ("add" not "added" or "adds")
   - Don't capitalize first letter
   - No period at the end
   - Keep subject concise (50 characters or less)
   - If changes are complex, add a blank line followed by a body explaining what and why
   - If breaking change, add `!` after type: `type!: subject`

8. **Create the commit** using a HEREDOC for proper formatting:
    ```bash
    git commit -m "$(cat <<'EOF'
    type: subject

    Optional body with more details.
    EOF
    )"
    ```

9. **Push to remote**: `git push`
    - Only ask for confirmation if push fails

10. **Verify** by running `git log -1` to show the created commit

## Important

- Do NOT stage files that contain secrets (.env, credentials, etc.)
- Do NOT add any AI/Claude attribution, footers, or Co-Authored-By lines to commit messages
- Always check for and remove binary files from remote first
- Always analyze git diff --cached first to understand changes
- Generate description from actual code changes
