# bron-api-toolkit

> **This is an internal Go library, not a runnable program.** To *use* the Bron MCP server, run
> `bron mcp` from [bron-cli](https://github.com/bronlabs/bron-cli), or open **Bron Desktop → MCP Server**.

Shared, spec-driven API tooling generated from the Bron public API's OpenAPI spec:

- **catalog** — the API distilled into data (resources, verbs, path/query params, body fields, types,
  enums, descriptions) plus the embedded spec.
- **helpers** — request/response utilities the CLI and MCP tools both use (query params, request bodies,
  output projection, date humanization, jq filtering, embeds, untrusted-field wrapping).
- **mcptools** — builds MCP tools from the catalog at runtime.

Consumed by `bron-cli` (its command tree and `bron mcp`) and by Bron Desktop's embedded MCP server, so
the agent tooling never diverges between them.

## Updating the spec

```bash
cp <path>/bron-open-api-public.json .
make generate   # rewrites catalog/helpdoc.go, catalog/spec.go, catalog/spec.json
make test
```

`TestExternalTextPathsPinned` lists every field the spec marks `format: external-text`; a spec that adds one
fails it until the new path is added to the expected list. Tag a release (`vX.Y.Z`), then bump the dependency in
the consumers.
