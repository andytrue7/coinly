# 1. Monorepo over polyrepo

## Status

Accepted

## Context

Coinly is built solo, as a set of independently deployable services
(identity, wallet, and more in later phases) sharing proto contracts and a
small amount of infra code (`pkg/`). A polyrepo (one git repo per service)
is the conventional choice once a team and CI infra exist to support it,
but it also brings real overhead: cross-repo versioning of shared protos
and `pkg/`, coordinating PRs that touch a contract and both its producer
and consumer, and N separate CI/release pipelines to maintain.

## Decision

Use a single monorepo for all services, `proto/`, `gen/go/`, and `pkg/`.
Each service still gets its own Go module (`services/identity/go.mod`,
`services/wallet/go.mod`, ...) so they build, version, and are deployed
independently — the monorepo is a source-control and CI convenience, not a
build-system merge. CI uses path-filtered jobs (`dorny/paths-filter`) so a
change to one service doesn't trigger the whole matrix, deliberately
practicing the same targeted-CI skill a polyrepo-at-scale would otherwise
need.

## Consequences

- A contract change (proto) and its consumers can land in one PR, reviewed
  and tested together — no cross-repo coordination for a solo developer.
- One `git log`, one issue tracker, one place to search the whole system.
- CI must be path-filtered deliberately, or every change re-runs every
  service's pipeline; this is called out as a Phase 1 requirement, not an
  afterthought.
- If this project ever grew a multi-team org around it, the monorepo
  decision would likely be revisited — that tradeoff is accepted as
  out of scope for a solo portfolio project.

## Alternatives considered

- **Polyrepo** (one repo per service): rejected as pure overhead for a
  solo developer — no team boundaries to enforce, and it would spread the
  proto-contract and `pkg/` versioning problem across repos instead of
  solving it once with path-filtered CI.
