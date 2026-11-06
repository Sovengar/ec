# Third-party notices

## ec (chojs23/ec, "ec2")

Parts of this project are derived from https://github.com/chojs23/ec:

- `internal/engine`
- `internal/gitutil`
- `internal/markers`
- `internal/mergeview`
- `internal/gitmerge`
- `internal/cli`
- `internal/run`
- `internal/tui`

Local changes: import paths, `ApplyText`, diff3 base restoration on import,
inline editor (`e`), external editor moved to `E`, AI (`A`) and abort (`X`)
hooks and their tests.

MIT License, Copyright (c) 2024-2026 chojs23. Full text in `LICENSE.ec2`.

## agentbridge (sovengar/agentbridge)

`internal/agent` is the only package that imports
https://github.com/sovengar/agentbridge (MIT). It provides the harness catalog,
per-harness argv capabilities, model/effort catalogs and the headless
`ProcessInvoker`. Its `go 1.26.3` directive is why this module's floor moved.
