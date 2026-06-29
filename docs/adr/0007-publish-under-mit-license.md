# ADR 0007: Publish the Repository Under the MIT License

## Status

Accepted.

## Context

FulfillHub is being prepared as a public technical asset. The code, contracts,
benchmarks, and runbooks are already written as reusable engineering evidence,
but without an explicit license the default copyright position makes reuse and
internal study legally ambiguous.

## Decision

Publish the repository under the MIT License and expose that decision in the
README so reviewers know the implementation and documentation are intentionally
reusable.

## Consequences

- Readers can study, adapt, and fork the repo without guessing the allowed
  reuse boundary.
- Portfolio evaluation stays aligned with the repo's actual goal as an
  operational backend reference, not just a closed code sample.
- The permissive license does not change dependency licenses; those remain
  governed by their own terms.
- Future contributions must preserve copyright notices and license text.
