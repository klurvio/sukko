---
name: specify
description: Create a feature specification from a natural language description. Defines WHAT to build (not HOW).
user-invocable: true
---

# Create Feature Specification

Create a structured specification from a natural language feature description.

## Usage

```
/specify [feature description]
```

Examples:
- `/specify add per-tenant rate limiting to gateway`
- `/specify` - Will ask for a feature description

## Instructions

1. **Determine feature description**:
   - If provided as argument (`{{args}}`), use that description
   - If no description is provided, ask the user what they want to build

2. **Generate a concise branch name** (2-4 words):
   - Extract the most meaningful keywords from the description
   - Use prefix-noun format (e.g., `feat/tenant-rate-limiting`, `fix/kafka-offset-reset`)
   - Preserve technical terms and acronyms

3. **Create branch and directory**:
   - Create and checkout branch: `git checkout -b [prefix/short-name]`
   - Create spec directory: `specs/backlog/[branch-name]/`

4. **Write the specification** to `specs/backlog/[branch-name]/spec.md`:

   ```markdown
   # Feature Specification: [FEATURE NAME]

   **Branch**: `[branch-name]`
   **Created**: [DATE]
   **Status**: Draft

   ## Context
   [Why this feature is needed — the problem or opportunity]

   ## User Scenarios

   ### Scenario 1 - [Title] (Priority: P1)
   [User journey or system behavior in plain language]
   **Acceptance Criteria**:
   1. **Given** [state], **When** [action], **Then** [outcome]

   ### Edge Cases
   - What happens when [boundary condition]?

   ## Requirements

   ### Functional Requirements
   - **FR-001**: System MUST [capability]

   ### Non-Functional Requirements
   - **NFR-001**: [Performance/reliability/security requirement]

   ### Key Entities (if data involved)
   - **[Entity]**: [What it represents, key attributes]

   ## Success Criteria
   - **SC-001**: [Measurable, verifiable metric]

   ## Out of Scope
   - [Explicitly excluded items]
   ```

5. **Handle unclear aspects**:
   - Make informed guesses based on codebase context
   - Only use `[NEEDS CLARIFICATION: question]` if the choice significantly impacts scope
   - **Maximum 3 clarification markers**

6. **Validate the spec**:
   - No implementation details (no specific Go packages, Helm values, etc.)
   - Focused on system behavior and outcomes
   - Requirements are testable and unambiguous
   - Success criteria are measurable

7. **Create timestamp marker** in the spec directory:
   - Create an empty file named `CREATED_MM-DD-YYYY_HH-MM` (no extension) using the current date/time
   - Example: `specs/backlog/feat/tenant-rate-limiting/CREATED_03-21-2026_15-30`
   - This allows checking spec creation time from a directory listing without opening files

8. **Report completion** with branch name, spec file path, and readiness for next phase (`/clarify` or `/plan-feature`)

## Notes

- Focus on **WHAT** the system should do and **WHY** — avoid **HOW** to implement
- Each scenario should be independently testable
- Scenarios ordered by priority (P1 = most critical)
- Use reasonable defaults for unspecified details
