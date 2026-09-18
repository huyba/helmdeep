# End-to-end tests

`gateway_test.go` runs a fully wired Tool Gateway — real HTTP, the actual
`examples/policies/` bundle, a real hash-chained audit log
(`pkg/audit.FileStore` against a temp file), and four
`internal/mockupstream` fixtures standing in for real systems — exactly as
an unmodified MCP client would, over the wire (correct headers, correct
`_meta`, no shortcuts).

```sh
go test ./test/e2e/... -v
```

Covers: `tools/list` filtered by scope, a scope-based deny, an
unknown-tool protocol error, the taint policy allowing a trusted-sourced
value and denying an email-sourced one, the aggregate rate limit tripping
after its threshold, an unresolved-credential deny, and a final
`Verify()` over the resulting audit log.
