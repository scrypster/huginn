#!/usr/bin/env python3
"""Fail if OpenAPI routes drift from registered HTTP routes.

Compares `/api/v1/*` routes in `internal/server/server.go` against
path+method operations declared in `docs/reference/openapi.yaml`.
"""

from __future__ import annotations

import pathlib
import re
import sys
from collections import Counter


ROUTE_PATTERN = re.compile(r'mux\.HandleFunc\("([A-Z]+) (/api/v1/[^"]+)"')
OPENAPI_PATH_PATTERN = re.compile(r"^  (/api/v1/[^:]+):\s*$")
OPENAPI_METHOD_PATTERN = re.compile(r"^    (get|post|put|patch|delete):\s*$")


def parse_server_routes(server_go: pathlib.Path) -> list[tuple[str, str]]:
    text = server_go.read_text(encoding="utf-8")
    routes: list[tuple[str, str]] = []
    for match in ROUTE_PATTERN.finditer(text):
        routes.append((match.group(1).lower(), match.group(2)))
    return routes


def parse_openapi_ops(openapi_yaml: pathlib.Path) -> tuple[list[str], list[tuple[str, str]]]:
    lines = openapi_yaml.read_text(encoding="utf-8").splitlines()
    paths: list[str] = []
    ops: list[tuple[str, str]] = []
    current_path = ""
    for line in lines:
        path_match = OPENAPI_PATH_PATTERN.match(line)
        if path_match:
            current_path = path_match.group(1)
            paths.append(current_path)
            continue
        method_match = OPENAPI_METHOD_PATTERN.match(line)
        if current_path and method_match:
            ops.append((method_match.group(1), current_path))
    return paths, ops


def main() -> int:
    repo_root = pathlib.Path(__file__).resolve().parents[1]
    server_go = repo_root / "internal/server/server.go"
    openapi_yaml = repo_root / "docs/reference/openapi.yaml"

    routes = parse_server_routes(server_go)
    paths, spec_ops = parse_openapi_ops(openapi_yaml)

    duplicate_paths = [path for path, count in Counter(paths).items() if count > 1]
    missing = [route for route in routes if route not in spec_ops]
    extra = [op for op in spec_ops if op not in routes]

    if not duplicate_paths and not missing and not extra:
        print(f"openapi parity ok: {len(routes)} route-method pairs")
        return 0

    print("openapi parity check failed")
    if duplicate_paths:
        print(f"\nDuplicate OpenAPI path keys ({len(duplicate_paths)}):")
        for path in sorted(duplicate_paths):
            print(f"  - {path}")
    if missing:
        print(f"\nMissing from OpenAPI ({len(missing)}):")
        for method, path in missing:
            print(f"  - {method.upper()} {path}")
    if extra:
        print(f"\nOpenAPI operations without matching route ({len(extra)}):")
        for method, path in extra:
            print(f"  - {method.upper()} {path}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
