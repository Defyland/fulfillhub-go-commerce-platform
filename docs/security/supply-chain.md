# Supply Chain Security

## Purpose

This document defines the release integrity controls expected for FulfillHub.
The repository already validates code, dependencies, container images, OpenAPI,
markdown, secrets, and SBOM generation in CI. Production release workflows
should add signed artifacts and provenance.

## Current automated controls

- `go test ./...`, `go vet ./...`, and race detection
- `govulncheck` against the Go call graph
- Gitleaks secret scanning with full git history checkout
- Trivy filesystem and image scanning for high and critical findings
- SBOM generation for the built container image
- Docker build and Compose config validation
- OpenAPI linting and benchmark budget validation

## Release artifact policy

Every production image should have:

1. Immutable digest pinned to the release commit.
2. SBOM attached as a release artifact.
3. Trivy scan with no accepted high or critical unfixed findings.
4. Keyless Cosign signature bound to the GitHub Actions OIDC identity.
5. SLSA provenance statement for the image build.
6. Branch protection requiring all quality gates before promotion.

## Verification before deploy

Before applying production manifests:

```sh
cosign verify ghcr.io/defyland/fulfillhub-go-commerce-platform@sha256:<digest>
cosign verify-attestation ghcr.io/defyland/fulfillhub-go-commerce-platform@sha256:<digest>
```

The deployment platform should reject unsigned images or mutable tags.

## Exception handling

Security exceptions must include:

- CVE identifier or scanner rule
- affected package or image layer
- exploitability assessment for FulfillHub
- compensating control
- expiration date
- owner responsible for removal

Exceptions without expiration should not be accepted for production.
