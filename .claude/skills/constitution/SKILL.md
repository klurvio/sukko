---
name: constitution
description: Create or update the project constitution — the governing principles that shape all code.
user-invocable: true
---

# Manage Constitution

Create or update the project constitution — the set of non-negotiable principles governing how all code in this project is written.

## Usage

```
/constitution [action or principle updates]
```

Examples:
- `/constitution` - Review the current constitution
- `/constitution add principle about graceful shutdown` - Add a new principle
- `/constitution update VII to include channel safety` - Amend existing principle

## Instructions

1. **Check for existing constitution**:
   - Read `CLAUDE.md` at the project root — look for a `## Constitution` section
   - If it exists, you're amending. If not, you're creating from scratch.

2. **Collect principles from user input**:
   - If the user provided principles in their input (`{{args}}`), use those
   - If not, analyze existing code patterns (CODING_GUIDELINES.md, codebase conventions) and propose principles
   - Ask the user to confirm or adjust before writing
   - The user might want fewer or more principles — respect that

3. **Draft the constitution** with this structure:

   ```markdown
   ## Constitution

   **Version**: X.Y.Z | **Ratified**: YYYY-MM-DD | **Last Amended**: YYYY-MM-DD

   ### I. [Principle Name]
   [Description — declarative, testable, uses MUST/SHOULD language]

   ### Governance
   - Constitution supersedes all other practices
   - Amendments require documentation and version bump
   - All code changes must verify compliance
   ```

4. **Version the constitution**:
   - New constitution: Start at `1.0.0`
   - Adding/expanding principles: bump MINOR (e.g., 1.1.0)
   - Wording/clarification changes: bump PATCH (e.g., 1.0.1)
   - Removing or redefining principles: bump MAJOR (e.g., 2.0.0)

5. **Write the constitution**:
   - Update the `## Constitution` section in `CLAUDE.md`

6. **Report completion**:
   - New version and bump rationale
   - List of principles
   - Suggested commit message

## Notes

- Principles must be declarative and testable — no vague language
- Replace "should" with "MUST" or "SHOULD" with explicit rationale
- Keep principles concise but specific
- Each principle needs a clear name and actionable rules
- Maximum ~12 principles to keep it manageable
- Dates in ISO format (YYYY-MM-DD)
