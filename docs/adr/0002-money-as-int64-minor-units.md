# 2. Money as int64 minor units, no floats

## Status

Accepted

## Context

Coinly's core is a double-entry ledger: journal postings must sum to
exactly zero per currency, and balances must never silently drift.
Floating-point (`float32`/`float64`) representations of money are a
well-known source of rounding and representation-error bugs (e.g.
`0.1 + 0.2 != 0.3` in IEEE 754) that are unacceptable when the invariant
being enforced is an exact zero-sum.

Different currencies also have different numbers of minor units per major
unit (EUR has 2 decimal places — cents; JPY has 0 — no subunit), so a
single fixed "cents" assumption isn't correct either.

## Decision

Represent every monetary amount as a `pkg/money.Money{amount int64,
currency string}` value, where `amount` is always in the currency's
**minor units** (e.g. EUR amount `1050` = €10.50; JPY amount `1050` =
¥1050). A per-currency exponent table (ISO 4217-derived: EUR=2, JPY=0, ...)
converts to/from major-unit display strings only at the edges (API
responses, logs) — internal arithmetic never touches a float.

`Money` exposes `Add`, `Sub`, `Neg`, and `Split` (for allocating an amount
across N recipients without losing or gaining minor units to rounding).
Currency exchange rates are represented as scaled integers (×10⁶) rather
than floats, using round-half-even, with any remainder from a split or
conversion posted to the `fx_liquidity` system account rather than
dropped — so the ledger's zero-sum invariant holds even across FX
conversions.

## Consequences

- No float appears anywhere in money-handling code; this is enforceable by
  code review and by never importing a float type in `pkg/money` or any
  domain package that uses it.
- Every currency's exponent must be looked up (not assumed) before
  converting to/from a major-unit display value — adding a new currency
  means adding a table entry, not writing new conversion logic.
- `Split` and FX conversion need explicit remainder-handling logic (posted
  to `fx_liquidity`), which is more code than "let the float rounding
  happen" — accepted, because the alternative breaks the ledger invariant.

## Alternatives considered

- **`float64` amounts**: rejected outright — not exact, and this project's
  centerpiece invariant (postings sum to zero) requires exactness.
- **`decimal.Decimal` (arbitrary-precision) library**: workable, but adds
  a dependency and allocation overhead for a value type used everywhere in
  the hot path (every posting, every balance check); int64 minor units is
  the standard fintech-ledger representation and needs no external type.
