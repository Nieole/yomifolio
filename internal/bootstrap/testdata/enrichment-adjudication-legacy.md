# Legacy adjudication provenance fixture

`enrichment-adjudication-legacy.db.gz` is a closed real SQLite database produced by
production `bootstrap.Open` at **81a8807fa35c445f4886ca8ee5c6caf850040ee8**, whose
field provenance records no writing operation. The adjacent JSON holds the web
login, the series and proposal IDs, the summary receipt as read back through
`GET /api/mcp/operations/{id}`, the tags operation ID and the provenance list as
served by `GET /api/series/{id}/metadata-review`.

Generation used the `TestProductionMCPEnrichmentAuditUndo` setup with one controlled
source result, through the generic MCP SDK:

1. Propose `summary`, `tags` and `rating` from `bangumi:0` with identity and edition
   confirmed.
2. Adjudicate `summary` with `mode=partial` (request key `legacy-summary`).
3. Adjudicate `tags` with `mode=partial` (request key `legacy-tags`); `rating` stays
   pending.
4. Close MCP, HTTP, the production graph and SQLite; gzip the database bytes. No
   rows or receipt payloads were synthesized or rewritten.

The fixture is static: tests read it, never regenerate it with the code under test.

Fixture SHA-256:

- db.gz: `1ebc6bad2579cfe4a9da4e42e41c72b64a8f2fe95f3b0c6f3c2bafb07dba361b`
- json: `72a9574e1ee78cfed62ca28703184b4baae680ef167f0180c495bc9cec8df945`
