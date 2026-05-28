# ADR 0003: Split Merchant and Operations Authentication

- Status: accepted
- Date: 2026-05-28

## Context

FulfillHub serves two materially different actor classes: merchant systems calling the checkout API and internal operators handling cancellations, replay, and investigations. Their trust boundaries, audit needs, and rate-limiting profiles are different.

## Decision

Merchant-facing endpoints will use scoped API keys. Operations-only paths will use JWT bearer tokens with role claims. Authorization checks will enforce `merchant_id` scoping for merchant traffic and role-based access for operator workflows.

## Consequences

- Positive: machine-to-machine merchant traffic stays simple for storefront integrations
- Positive: operator actions can be audited with richer identity and role context
- Positive: different rate limits and revocation controls can be applied to each actor class
- Negative: two authentication strategies increase documentation and implementation surface
- Negative: authorization tests become mandatory for both tenant and role boundaries
