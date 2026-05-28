# ADR 0001: Start With a Modular Monolith

- Status: accepted
- Date: 2026-05-28

## Context

FulfillHub needs to model a checkout workflow with strict consistency requirements across orders, inventory, payments, and shipments. Splitting those concerns into independently deployed services too early would make transaction boundaries, local development, and failure debugging harder before the domain is stable.

## Decision

Phase 1 will start as a modular monolith in Go. Modules will be isolated through explicit interfaces, package boundaries, and contract-first HTTP and event schemas, but they will be deployed as one service initially.

## Consequences

- Positive: simpler local development, fewer operational moving parts, easier cross-module refactoring, and faster learning about the domain
- Positive: transaction-sensitive flows remain easier to reason about while the saga model settles
- Negative: a future split into separate services will require careful extraction of data ownership and deployment boundaries
- Negative: internal module discipline must be maintained intentionally or the monolith will degrade into tight coupling
