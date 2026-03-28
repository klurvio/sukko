---
name: checklist
description: Generate a custom requirements quality checklist for Go/infrastructure changes. Validates that requirements are complete and clear.
user-invocable: true
---

# Create Requirements Checklist

Create domain-specific checklists that test whether **requirements are well-written**, NOT whether the implementation works.

## Usage

```
/checklist [domain]
```

Examples:
- `/checklist performance` - Create a performance requirements checklist
- `/checklist security` - Create a security requirements checklist
- `/checklist concurrency` - Create a concurrency safety checklist
- `/checklist deployment` - Create a deployment requirements checklist

## Instructions

1. **Load feature context**:
   - Get current branch: `git branch --show-current`
   - Resolve spec directory by searching in order (first match wins):
     1. `specs/in-progress/[branch-name]/`
     2. `specs/backlog/[branch-name]/`
     3. `specs/completed/[branch-name]/`
     4. `specs/[branch-name]/` (legacy fallback)
   - Load `{resolved-spec-dir}/spec.md` and `plan.md`
   - Read constitution from `CLAUDE.md`

2. **Clarify intent** (up to 3 questions):
   - Derive from user input + spec signals
   - Only ask what materially changes checklist content
   - Skip if already clear from `{{args}}`

3. **Generate checklist** with items grouped by quality dimension:

   | Dimension | What it checks |
   |-----------|---------------|
   | Completeness | Are all necessary requirements present? |
   | Clarity | Are requirements specific and unambiguous? |
   | Consistency | Do requirements align without conflicts? |
   | Measurability | Can requirements be objectively verified? |
   | Coverage | Are all scenarios/edge cases addressed? |
   | Config Coherence | Do env vars, Helm values, and Go struct tags align? |
   | Dependencies | Are infrastructure assumptions documented? |

4. **Domain-specific checks**:

   | Domain | Additional checks |
   |--------|------------------|
   | performance | Connection limits, rate limits, resource requests/limits, backpressure thresholds |
   | security | Input validation, JWT, tenant isolation, secrets management, rate limiting |
   | concurrency | Goroutine lifecycle, mutex scope, atomic usage, channel safety, panic recovery |
   | deployment | Helm values, Terraform variables, image tags, rollback plan, health checks |
   | kafka | Consumer groups, topic naming, offset management, partition strategy, backpressure |
   | monitoring | Prometheus metrics naming, label cardinality, dashboard queries, alert thresholds |

5. **Item format**:
   ```
   - [ ] CHK001 Are [requirement type] defined for [scenario]? [Quality Dimension]
   ```

6. **Write checklist** to `{resolved-spec-dir}/checklist-[DOMAIN].md`

7. **Report**: File path, item count, focus areas, suggested next steps

## Prohibited Patterns

- "Verify", "Test", "Confirm" + implementation behavior
- References to code execution or system behavior
- Implementation details (specific Go functions, Helm templates)

## Required Patterns

- "Are [requirement type] defined/specified/documented for [scenario]?"
- "Is [vague term] quantified with specific criteria?"
- "Are requirements consistent between [spec section] and [task list]?"
- "Can [requirement] be objectively measured/verified?"

## Notes

- Checklists are "unit tests for requirements writing" — they test the quality of the spec, not the code
- Each `/checklist` run creates a NEW file (never overwrites existing ones)
