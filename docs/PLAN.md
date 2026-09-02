# Coinly — Senior-Level Fintech Portfolio Project

## Context

The user (a backend developer targeting senior level) wants a learn-by-doing fintech project for their CV, covering: Go, REST, gRPC, PostgreSQL, MongoDB, Kafka, Redis, Docker, Kubernetes, CI/CD, Makefile, golangci-lint, microservices, saga, event-driven, hexagonal architecture. Decisions made with the user:

- **Product:** *Coinly* — a digital wallet & payments platform (Wise/Revolut core): multi-currency wallets, P2P transfers, deposits/withdrawals via a simulated provider, currency exchange, double-entry ledger.
- **Delivery:** phased roadmap — full target architecture designed up front, built in milestones, each phase ending demoable. Deep detail for Phase 1; later phases as outline.
- The directory `/Users/andriitrubchanynov/Projects/practice/go/coinly` is empty (greenfield). Not yet a git repo — `git init` is step one.

## Target architecture (end state)

### Services & data ownership

| Service | Responsibility | Store | Why |
|---|---|---|---|
| **identity** | Register/login, JWT (access+refresh), JWKS endpoint | Postgres | Relational, unique email, transactional |
| **wallet** | Wallets, **double-entry ledger**, balances, atomic P2P, holds | Postgres | ACID non-negotiable; the heart of the project |
| **payments** | Deposits/withdrawals, **withdrawal saga orchestrator**, payment-intent idempotency | Postgres | Saga state + outbox in one tx |
| **provider-sim** | Fake bank/card provider: async callbacks, configurable delays/failures | Redis | Deliberate black box |
| **exchange** | FX rates (simulated feed), TTL rate quotes, rate history | Mongo + Redis | Append-heavy flexible docs; Redis for current rates + quote TTLs |
| **history** | CQRS read model: per-user transaction feed projected from Kafka | Mongo | Denormalized read-optimized docs, rebuildable by replay |
| **notification** | Consumes events, "sends" & logs notifications, idempotent consumer | Mongo | Fire-and-forget doc stream |
| **gateway** | Single REST edge: routing, JWT verify, rate limiting (Phase 5) | Redis | One public surface; internal traffic = gRPC |

Redis cross-cutting: idempotency fast path, rate limits, FX quotes, refresh-token sessions.

### Communication rules

- **Commands are sync, facts are async.** Client→gateway REST; gateway→services and service→service gRPC; domain events over Kafka, always via **transactional outbox**.
- No sync auth calls: services verify JWT locally via cached JWKS.
- Topics: `wallet.events`, `payments.events`, `exchange.events` (protobuf-encoded, `buf breaking`-gated).

### Saga decisions (ADR-worthy)

- **P2P same-currency transfer: NOT a saga** — one ACID tx in wallet (senior insight: don't distribute what a boundary already makes atomic).
- **Withdrawal: orchestrated saga** (payments owns a persisted state machine): CreateWithdrawal → wallet.CreateHold → provider-sim (async) → on success wallet.CaptureHold / on failure-or-timeout wallet.ReleaseHold. Orchestration chosen: clear owner, timeouts, compensation in one place.
- **Deposit: choreography-flavored** event flow (no compensation needed) — teaches the other style.
- **FX exchange: quote-then-atomic-post** (signed TTL quote in Redis, then one atomic multi-currency journal). No saga.

### Double-entry ledger (core design)

- **Accounts** are the primitive (per wallet+currency, plus system accounts: `external_clearing`, `fees_revenue`, `fx_liquidity`).
- Immutable **journal entries** with ≥2 **postings**; per currency, postings sum to **zero** (signed int64 minor units).
- Balances derivable via SUM but cached in `account_balances` (same tx, optimistic version) + periodic reconciliation asserting cache == sum.
- **Holds** = postings to a `user_held` sub-account; available = balance − held. Corrections are reversal journals, never UPDATEs.

## Repo strategy

**Monorepo** (polyrepo is pure overhead solo; path-filtered CI is itself a senior skill). Layout:

```
coinly/
├── Makefile  go.work  .golangci.yml  README.md
├── .github/workflows/ci.yml
├── docs/adr/  docs/diagrams/
├── proto/  (buf-managed; coinly/identity/v1, coinly/wallet/v1, coinly/events/v1)
├── gen/go/  (buf-generated, own module)
├── pkg/     (own module; infra-only: money/, ids/, httpx/, grpcx/, pgx/; later outbox/, kafkax/)
├── services/identity/  services/wallet/  (each its own Go module; more later)
├── deploy/compose/docker-compose.yml  deploy/k8s/ (Phase 5)
└── tools/
```

Hexagonal layout per service (wallet as reference):

```
services/wallet/
├── cmd/wallet/main.go        # wiring only
├── migrations/               # goose SQL
├── internal/config/
├── internal/domain/          # PURE: wallet.go, ledger.go (Balanced() invariant), hold.go, errors.go
├── internal/app/             # use cases + ports/{in,out}.go; unit-tested with fakes
└── internal/adapters/
    ├── in/httpapi/  in/grpcapi/
    └── out/postgres/         # later out/kafka (outbox writer)
```

Anti-distributed-monolith rules: services talk only via proto contracts; no cross-service `internal/` imports (Go enforces); `pkg/` = infra + the single shared value type `money`; each service owns its schema; `depguard` machine-enforces layer boundaries (domain imports nothing → app → adapters → cmd).

## Phase 1 — Foundation (build this now)

**Services: identity + wallet.** No gateway yet (each exposes REST directly). Wallet includes a clearly-flagged demo **top-up faucet** endpoint so P2P is demoable before payments exists.

**Demo (definition of done):** `docker compose up` → register Alice & Bob → EUR wallets → top up Alice → Alice sends Bob €25 with an `Idempotency-Key` → balances correct, journal visible, replayed request returns the same transfer.

### Build order

1. **Scaffolding:** `git init`, `go.work`, root Makefile, `.golangci.yml`, CI skeleton, ADRs 0001–0003, README with architecture diagram. CV-presentable from the first commits.
2. **`pkg/money`:** `Money{amount int64 minor units, currency}` + Add/Sub/Neg/Split, exponent table (EUR=2, JPY=0), exhaustive table-driven tests. No floats anywhere.
3. **proto + buf:** buf.yaml/buf.gen.yaml, identity/v1 + wallet/v1; `make proto`; `buf lint` + `buf breaking` in CI.
4. **identity:** domain → app (Register/Login/Refresh/Logout) → Postgres adapter → REST → JWKS. Ed25519 keys; access 15 min, refresh 30 d (hashed, revocable).
5. **wallet domain:** entities + `Balanced()` zero-sum invariant + domain tests (incl. multi-currency journal case).
6. **wallet app layer:** use cases vs port interfaces, unit tests with hand-written fakes.
7. **wallet Postgres adapter:** goose migrations, repos, UnitOfWork (tx-scoped repos), `SELECT … FOR UPDATE` ordered by account id (deadlock avoidance).
8. **wallet REST + gRPC adapters**, JWT middleware (JWKS cached).
9. **Integration tests** (testcontainers): repo round-trips; **concurrency test — 50 parallel transfers → exact final balances, no negative available**; balance-vs-sum reconciliation assertion.
10. **docker-compose + seed script + README demo walkthrough.**

### API surface

- **identity REST:** `POST /v1/auth/{register,login,refresh,logout}`, `GET /v1/users/me`, `GET /.well-known/jwks.json`. gRPC: `UserService.GetUser` (internal, for later enrichment).
- **wallet REST** (auth; mutations require `Idempotency-Key`): `POST/GET /v1/wallets`, `GET /v1/wallets/{id}`, `POST /v1/transfers`, `GET /v1/transfers/{id}`, `GET /v1/wallets/{id}/entries?cursor=`, `POST /v1/wallets/{id}/topups` (demo faucet, gated out in Phase 3).
- **wallet gRPC** (defined now, consumed by payments in Phase 3): `GetBalance, CreateHold, CaptureHold, ReleaseHold, PostJournal`.

### DB schemas (sketch)

- identity: `users(id, email citext unique, password_hash, status, timestamps)`, `refresh_tokens(id, user_id, token_hash unique, expires_at, revoked_at)`.
- wallet: `wallets(id, user_id, currency, status, unique(user_id,currency))`; `accounts(id, type ['user_available'|'user_held'|'external_clearing'|'fees'|'fx_liquidity'], wallet_id?, currency, unique(wallet_id,type,currency))`; `journal_entries(id uuidv7, kind, idempotency_key unique, initiated_by, note, created_at)` — append-only; `postings(id identity, journal_id, account_id, amount bigint ≠0 signed minor units, currency)` — per (journal,currency) SUM=0, enforced in domain + deferred trigger; `account_balances(account_id pk, balance, version, updated_at)` — updated same tx, `balance >= 0` check for user_available; `transfers(...)` thin API projection.

### Tooling

- **compose:** postgres:17 (init script creates `identity` + `wallet` DBs), redis:7, both services, one-shot goose `migrate` profile; healthchecks + `service_healthy` deps. No Kafka/Mongo yet.
- **Makefile:** `help lint test test-integration proto proto-breaking build up down migrate-up migrate-create seed ci`.
- **golangci-lint:** errcheck, govet, staticcheck, revive, gosec, sqlclosecheck, rowserrcheck, errorlint, exhaustive, copyloopvar, **depguard (hexagonal boundaries)**, goimports, misspell, unparam, unconvert, gocritic.
- **CI (GitHub Actions):** `dorny/paths-filter` per service → matrix lint/test (`go test -race -cover`) per changed module; buf lint+breaking; testcontainers integration job; docker build per changed service → GHCR on main; Go module+build caching.
- **Testing:** domain/app = pure unit, table-driven, hand-written fakes; adapters = testcontainers-go Postgres with programmatic goose migrations; API = httptest against fully wired app (happy path, auth failures, idempotent replay).

## Phases 2–6 (outline; refine when reached)

- **Phase 2 — Kafka + outbox + first consumers.** Kafka (KRaft) in compose; `pkg/outbox` poller; wallet publishes `JournalPosted`/`TransferCompleted`; **notification** service (Mongo enters) with idempotent handlers (processed-message table); protobuf event envelope (event_id UUIDv7, occurred_at, trace_context). *DoD:* kill Kafka mid-transfer → no lost/dup events after restart.
- **Phase 3 — Payments + withdrawal saga.** **provider-sim** (configurable failure rate, async callbacks) + **payments** orchestrator (persisted state machine, timeout sweep for compensation); wallet Hold RPCs go live; end-to-end idempotency (Redis SETNX fast path + DB unique as truth); faucet gated off. *DoD:* withdrawals at 30% simulated failure → every failure releases its hold; ledger reconciles to zero.
- **Phase 4 — Exchange + CQRS history.** **exchange** (Mongo rate history, Redis rates + TTL quotes, `RateUpdated` feed); wallet `ExchangeMoney` (quote validation + atomic multi-currency journal); **history** projecting Kafka→Mongo, cursor-paginated `GET /v1/history`, rebuildable by replay. *DoD:* drop history collection, replay from earliest offset, identical feed.
- **Phase 5 — Gateway + Kubernetes + observability.** gateway (JWT verify, routing, Redis rate limiting, request IDs); kind/minikube + kustomize overlays, probes, HPA, secrets; OpenTelemetry middleware in `pkg/` → Jaeger traces, Prometheus+Grafana dashboards (ledger throughput, saga states, consumer lag), slog with trace correlation. *DoD:* one Jaeger trace spanning gateway→payments→wallet→Kafka→notification.
- **Phase 6 — Hardening & performance.** Circuit breaker (sony/gobreaker) payments→provider-sim, retries with backoff/jitter, deadline propagation, graceful-shutdown audit; k6 load scenarios (P2P throughput, hot-wallet contention) with numbers in README; security pass (gosec, distroless); chaos demo (kill wallet pod mid-saga → recovery). *DoD:* documented load-test numbers + "failure drill" README section.

## ADRs to write (docs/adr/)

1. Monorepo; 2. Money = int64 minor units + ISO-4217, no floats (FX rates as scaled integers ×10⁶, round-half-even, remainder→fx_liquidity); 3. UUIDv7 IDs; 4. Double-entry append-only ledger + balance cache + reconciliation + holds-as-sub-accounts; 5. Idempotency (client `Idempotency-Key`, Redis fast path + DB unique as truth, replay returns original response); 6. JWT EdDSA + JWKS, rotating hashed refresh tokens, local verification (PASETO considered); 7. Protobuf event contracts, additive-only via `buf breaking`; 8. Saga strategy incl. *when not to saga*; 9. No gateway until Phase 5.

## Anti-goals (deliberately NOT building)

Real provider/bank integrations, KYC/AML, any frontend/mobile (curl/httpie demo script instead), multi-region HA, service mesh, k8s operators, admin backoffice, real email/SMS, anything PCI-scoped (never touch card numbers, even fake — opaque provider tokens only).

## Risks

- Scope creep → every phase ends demoable; main is always green; DoD checklists in README.
- Ledger bugs → invariant in three layers (domain, DB trigger, reconciliation job) + concurrency test from Phase 1.
- Solo microservice overhead → only 2 services until Phase 3; shared `pkg/` middleware; compose stays the dev loop.
- Resume-driven tech → every store/broker enters in the phase that needs it, reason recorded in an ADR.
- Time → each phase is standalone CV value; stopping after Phase 3 already yields "Go microservices, double-entry ledger, Kafka outbox, orchestrated saga."

## Verification (Phase 1)

1. `make ci` green locally (lint + unit tests + build); `go test -race` clean.
2. `make test-integration` — testcontainers suite incl. the 50-parallel-transfers race test and balance==SUM(postings) reconciliation.
3. `make up && make seed` → run the README demo script: register two users, wallets, top-up, P2P transfer with idempotency-key replay → assert both balances and that replay returns the same transfer id.
4. CI passes on GitHub on first push (path filters, buf checks, docker builds).