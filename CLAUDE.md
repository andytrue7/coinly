# CLAUDE.md

Project overview, tech stack, and architecture: see [README.md](README.md) and [docs/PLAN.md](docs/PLAN.md)

## Plan updates 
- docs/PLAN.md contains the phased plan with a Status checklist. 
- When a phase/milestone is completed and its work is committed, update the Status checklist in docs/PLAN.md to mark it done, before moving on to the next phase. 
- If during work you discover the plan needs to change (a phase should be split, reordered, or the schema/API sketch needs revising), stop and flag it to me before editing PLAN.md — don't silently rewrite the plan.

## Explain your reasoning
- Before writing non-trivial code, briefly explain your planned approach
  (3-5 sentences) and why you're choosing it over alternatives. Give me the reasoning and at least one alternative you didn't pick and why not. Skip this for boilerplate/scaffolding/config — just write those.

## Size of changes
- Work in small, reviewable increments. One logical change per commit.
  Don't implement multiple features or files in one shot unless I ask you to.
- After each small chunk works, stop and let me review before continuing.

## Git workflow
- Don't commit to `main` — branch first.
- Propose a clear, conventional commit message for each change (one logical change per commit). 
- If a task would touch many files or mix concerns, tell me first and suggest how to split it into smaller commits before starting. 

## Write tests first, then implement 
- Write comprehensive tests (unit/integration) before imlplementation in case if it makes sense to do this way when working on a feature


