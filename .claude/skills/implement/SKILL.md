---
name: implement
description: Execute the implementation plan by processing all tasks, phase by phase.
user-invocable: true
---

# Implement Feature

Execute all tasks from the task list, following the defined phases and dependencies.

## Usage

```
/implement
```

## Instructions

1. **Load implementation context**:
   - Get current branch: `git branch --show-current`
   - Load `specs/[branch-name]/tasks.md` and `specs/[branch-name]/plan.md`
   - Read constitution from `CLAUDE.md`

2. **Parse tasks** and extract:
   - Task phases, IDs, descriptions, file paths
   - Dependencies and parallel markers [P]
   - Execution order

3. **Execute phase by phase**:
   - Complete each phase before moving to the next
   - Respect dependencies — sequential tasks in order, parallel [P] tasks can overlap
   - Validate at each phase checkpoint

4. **Execution rules**:
   - Config first → Code → Infrastructure → Testing → Deploy & Verify
   - Mark completed tasks as `[x]` in the tasks file
   - Report progress after each completed task
   - Halt if a non-parallel task fails
   - For parallel [P] tasks: continue with successful ones, report failures

5. **Constitution compliance**:
   - All code MUST follow the constitution in `CLAUDE.md`
   - Verify error handling, concurrency safety, config externalization, metrics, etc.
   - Flag violations before committing

6. **Go-specific checks**:
   - Run `go vet ./...` after code changes
   - Run `go test ./...` to verify tests pass
   - Ensure new env vars match Helm chart values and deployment templates

7. **Completion validation**:
   - Verify all tasks completed
   - Check implementation matches original plan
   - Confirm tests pass
   - List deploy/verification commands

8. **Report**:
   - Summary of completed work
   - Any issues encountered
   - Test results
   - Suggested next steps (`/code-review`, deploy commands)

## Notes

- Mark completed tasks as `[x]` in the tasks file as you go
- Stop at any checkpoint to validate independently
- If tasks file is missing, suggest running `/generate-tasks` first
