---
name: clarify
description: Identify underspecified areas in the current feature spec by asking targeted clarification questions.
user-invocable: true
---

# Clarify Spec

Detect and reduce ambiguity in the active feature spec. This should run BEFORE planning.

## Usage

```
/clarify
```

## Instructions

1. **Find the active spec**:
   - Get current branch: `git branch --show-current`
   - Resolve spec directory by searching in order (first match wins):
     1. `specs/in-progress/[branch-name]/`
     2. `specs/backlog/[branch-name]/`
     3. `specs/completed/[branch-name]/`
     4. `specs/[branch-name]/` (legacy fallback)
   - Load `spec.md` from the resolved directory
   - If not found in any location, instruct user to run `/specify` first

2. **Load and scan the spec** for ambiguities across these categories:

   | Category | What to check |
   |----------|---------------|
   | Functional Scope | Core goals, out-of-scope declarations |
   | Data & Config | Env vars, Helm values, Terraform variables, types |
   | Infrastructure | Kubernetes resources, Redpanda topics, NATS subjects |
   | Performance | Connection limits, rate limits, resource requests/limits |
   | Integration | Service interactions, failure modes, backpressure |
   | Edge Cases | Crash recovery, rebalancing, pod preemption |
   | Constraints | DOKS limits, node pool sizing, provider quotas |

   Mark each: **Clear** / **Partial** / **Missing**

3. **Generate questions** (max 5 total):
   - Only ask questions whose answers materially impact architecture, configuration, or deployment
   - Prioritize by (Impact x Uncertainty) — highest first
   - Each question must be answerable with multiple choice (2-5 options) or short answer

4. **Ask ONE question at a time**:
   - For multiple-choice: recommend the best option with reasoning, then show all options
   - After answer: record it, move to next question
   - Stop when: all critical ambiguities resolved, user says "done", or 5 questions asked

5. **After EACH accepted answer**, update the spec:
   - Add/create a `## Clarifications` section
   - Append: `- Q: <question> → A: <answer>`
   - Update the relevant spec section
   - Save immediately after each integration

6. **Report completion**:
   - Number of questions asked & answered
   - Path to updated spec
   - Sections touched
   - Suggested next step (`/plan-feature`)

## Notes

- Maximum 5 questions per session
- Never reveal future queued questions
- If no meaningful ambiguities found: report "No critical ambiguities detected" and suggest proceeding
- Respect early termination signals ("stop", "done", "proceed")
- Never ask about tech stack — that's already defined
