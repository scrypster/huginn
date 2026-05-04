# API Reference

Huginn now includes an OpenAPI scaffold for core HTTP contracts:

- Spec file: `docs/reference/openapi.yaml`
- Version: `0.1.0`
- Scope: system health/config, sessions/messages, agents, workflows, connections, and skills management endpoints.

This contract is intentionally incremental. As handlers are hardened and covered by tests, additional routes should be added to the same spec to keep frontend/backend integration explicit and reviewable.

Route parity is enforced in CI via:

`python3 scripts/check_openapi_parity.py`
