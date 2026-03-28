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
   - Resolve spec directory by searching in order (first match wins):
     1. `specs/in-progress/[branch-name]/`
     2. `specs/backlog/[branch-name]/`
     3. `specs/completed/[branch-name]/`
     4. `specs/[branch-name]/` (legacy fallback)
   - Load `{resolved-spec-dir}/tasks.md` and `{resolved-spec-dir}/plan.md`
   - Read constitution from `CLAUDE.md`

2. **Move spec to in-progress** (if not already there):
   - If the spec was found in `specs/backlog/[branch-name]/`, move it to `specs/in-progress/[branch-name]/`
   - Create a `STARTED_MM-DD-YYYY_HH-MM` timestamp marker in the spec directory
   - If already in `specs/in-progress/`, skip this step

3. **Parse tasks** and extract:
   - Task phases, IDs, descriptions, file paths
   - Dependencies and parallel markers [P]
   - Execution order

4. **Execute phase by phase**:
   - Complete each phase before moving to the next
   - Respect dependencies — sequential tasks in order, parallel [P] tasks can overlap
   - Validate at each phase checkpoint

5. **Execution rules**:
   - Config first → Code → Infrastructure → Testing → Deploy & Verify
   - Mark completed tasks as `[x]` in the tasks file
   - Report progress after each completed task
   - Halt if a non-parallel task fails
   - For parallel [P] tasks: continue with successful ones, report failures

6. **Constitution compliance**:
   - All code MUST follow the constitution in `CLAUDE.md`
   - Verify error handling, concurrency safety, config externalization, metrics, etc.
   - Flag violations before committing

7. **Go-specific checks**:
   - Run `go vet ./...` after code changes
   - Run `go test ./...` to verify tests pass
   - Ensure new env vars match Helm chart values and deployment templates

8. **Completion validation**:
   - Verify all tasks completed
   - Check implementation matches original plan
   - Confirm tests pass
   - List deploy/verification commands

9. **Report**:
   - Summary of completed work
   - Any issues encountered
   - Test results
   - Suggested next steps (`/code-review`, deploy commands)

## Notes

- Mark completed tasks as `[x]` in the tasks file as you go
- Stop at any checkpoint to validate independently
- If tasks file is missing, suggest running `/generate-tasks` first
