# Base08 durable replay fixture

`enrichment-base08.db.gz` is a closed real SQLite database produced by production
`bootstrap.Open` at **c4540ce40dfd3b464dc462ed13b9c7b2005edac2**, before any 09 code.
The adjacent JSON contains the actual MCP requests, returned receipts and a
synthetic test-only credential (not a deployment secret).

Generation used the base `TestProductionMCPEnrichmentAuditUndo` setup and its
controlled metadata sources, through the generic MCP SDK, then:

1. Apply its original `first` request (summary/rating/tags/source_links).
2. Undo it with `base08-undo`.
3. Reapply the original payload with key `base08-unreversed`.
4. Close MCP, HTTP, production graph and SQLite; gzip the database bytes. No
   journal rows, hashes or receipt payloads were synthesized or rewritten.

The base source was loaded with a Go overlay (tracked 09 Go deltas replaced by
`git show c4540ce4:<path>`; the new authors implementation replaced by an empty
package). This did not move HEAD or modify the index. Generation succeeded under
Go1.26.10; local generation evidence is `.tmp/09/compat/base08-generate.log`.
The fixture is intentionally static: tests must not regenerate its expected hashes
using the current digest implementation. It contains temporary historical library
paths; no original directories, real network, or external providers are needed.

Original stored request hashes:

- first: `dd083f7e71418d6e6d2a69679c5d948b7e9fd2aa49a78f6b45a1b8942b86eb89`
- base08-undo: `f12d6afec6bdb89b795e4e90511fac9c53cd004b1a13e55e308eefd3bcfb1a41`
- base08-unreversed: `52d7b8eafc0a83a2b53fe021df4739ee2759843a0f51d15e4a88f71e65426f3d`

Fixture SHA-256:

- db.gz: `068a9aa4b15c7113ba07675be98ae18c923cac8f1cd5ab1136bf6aae2ec206b6`
- json: `ae545136fca342af08a51a9978f937b051b0103ebbed52853d5d04b6106ae565`
