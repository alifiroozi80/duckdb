# ADR 0001: Require Go 1.27 and Use the Official DuckDB Driver

- Status: Accepted
- Date: 2026-09-09

## Context

This module is a reusable GORM dialector for DuckDB. It previously required Go 1.24.2 and depended on the legacy Go driver v1.8.5, which embeds DuckDB 1.1.3. The driver moved to the DuckDB organization, and current releases use the versioned module path `github.com/duckdb/duckdb-go/v2`.

The migration changes the minimum Go version, embedded DuckDB engine, dependency graph, and supported build environments. These are observable compatibility decisions for downstream users.

## Decision

- Declare Go 1.27.0 as the minimum version in `go.mod`.
- Do not add a `toolchain` directive. CI selects the maintained Go 1.27 patch release, while downstream applications control their own toolchain.
- Use `github.com/duckdb/duckdb-go/v2` v2.10505.0, embedding DuckDB 1.5.5.
- Track latest stable tagged releases for direct Go dependencies and CI actions.
- Require CGO and verify the module on Debian 13, macOS, and Windows amd64. Use UCRT64 GCC on Windows.
- Do not claim FreeBSD support.

## Consequences

- Consumers must build this module with Go 1.27 or newer.
- Applications receive DuckDB 1.5.5 behavior, including the v2 driver's JSON scanning semantics and opt-in Arrow integration.
- Binaries continue to link the prebuilt DuckDB static library by default and remain larger than dynamically linked alternatives.
- CI takes longer because it exercises three operating systems and provisions a Windows C toolchain.
- Future Go, driver, and platform compatibility changes must update this ADR and the README together.

## Alternatives Considered

- Keep Go 1.24, matching the upstream driver's minimum. Rejected in favor of a single current Go baseline.
- Add a `toolchain` directive. Rejected because it has no effect when this module is consumed as a dependency and CI already selects the maintainer toolchain.
- Test only Linux. Rejected because the upstream driver ships platform-specific native libraries whose integration should be exercised.
- Keep the legacy driver or stop at the first repository-migration release. Rejected because the project should use the maintained official driver and latest stable DuckDB release.
