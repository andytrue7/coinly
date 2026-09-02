# 3. UUIDv7 for primary entity IDs

## Status

Accepted

## Context

Coinly's Postgres tables (users, wallets, journal entries, transfers, ...)
need a primary key strategy. Auto-increment integers leak sequence/volume
information across a public API and don't merge cleanly if data ever moves
between databases (e.g. reconciliation exports). Random UUIDs (v4) avoid
those problems but are bad B-tree citizens: inserting in random order
fragments the index and hurts write locality — which matters directly for
`journal_entries`, an append-only, high-write table at the center of this
project.

## Decision

Use **UUIDv7** (RFC 9562) for primary keys on entities that need a
globally-unique, externally-exposed ID — most notably `journal_entries.id`.
UUIDv7 embeds a millisecond timestamp in its high bits, so IDs generated
close together sort close together: inserts stay roughly append-order in
the primary key index (good B-tree locality, similar to an auto-increment
int) while keeping the non-guessability and merge-safety of a UUID.

## Consequences

- Journal entry IDs are both sortable-by-creation-time and safe to expose
  in API responses (no sequence-counting leak).
- Requires a UUIDv7 generator (Go's stdlib doesn't have one); `pkg/ids`
  wraps a small vetted library rather than every service picking its own.
- Slightly larger than a bigint PK (16 bytes vs 8) — accepted, since the
  locality and non-guessability benefits outweigh the storage cost at this
  project's scale.

## Alternatives considered

- **Auto-increment `bigserial`**: rejected — leaks row-count/volume
  information through exposed IDs, and doesn't survive a hypothetical
  future re-partitioning or cross-database merge cleanly.
- **UUIDv4**: rejected — good non-guessability, but random ordering
  fragments the primary-key B-tree on an append-heavy table like
  `journal_entries`, which is the highest-write-volume table in the
  system.
