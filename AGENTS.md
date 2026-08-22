# dgo contributor guidance

## Scope

- The root module is `github.com/darui3018823/dgo` and requires Go 1.26.6.
- `examples/linked_roles` and `examples/voice_receive` are independent Go
  modules. Keep their `go.mod` and `go.sum` valid whenever a change affects
  them.
- Treat the v1 public API as stable. Document any intentional compatibility
  change in `docs/Migration.md` and, when release-facing, in the appropriate
  file under `docs/releases/`.

## Implementation

- Format modified Go files with `gofmt`.
- Preserve the existing error handling, context propagation, and redaction
  conventions. Do not log tokens, authorization headers, encryption keys, or
  voice payload plaintext.
- Changes to Gateway or Voice lifecycle code must remain safe across reconnect,
  resume, leave, and rejoin flows.

## Validation

Run the checks that cover the changed code. The release baseline is:

```sh
go test -race ./...
go vet ./...

(cd examples/linked_roles && go test -race ./... && go vet ./...)
(cd examples/voice_receive && go test -race ./... && go vet ./...)

python -m pip install -r requirements-docs.txt
mkdocs build --clean --strict --site-dir site
```

Use `go test -coverprofile=coverage.out ./...` when changing coverage-sensitive
code. CI enforces minimum statement coverage of 31% (root), 60%
(`linked_roles`), and 40% (`voice_receive`).

## Documentation and releases

- Keep documentation links valid in both MkDocs and GitHub Releases. Release
  notes must use absolute GitHub URLs for links outside the release body.
- Release notes are written in English and stored in `docs/releases/<tag>.md`.
- Stable release tags must be validated by the release workflow before being
  announced.

## Git

- Keep commits small and focused. Conventional Commit messages in English are
  preferred.
- Commits may be created frequently when they form independently reviewable
  changes.
