# 11: State projection review boundary

Candidate: `7d21a845`. This is a consolidated review checklist, not a claim that a new Cartesian-product test suite ran. Actual execution evidence remains in `11-existing-proposal-adjudication.md`.

## One acceptance fact, all response paths

`proposal.Accepted` owns the fact: accepted field provenance, `applied_at`, and terminal state are considered without changing ordinary manual application semantics. An equal current metadata value alone is not proof of acceptance. Identity/version confirmation remains separate.

Review all of these together, not only the last failing constructor:

| Response path | Required source and invariant |
| --- | --- |
| ProposeMatch, new | Authorized queue transaction; no invented acceptance |
| ProposeMatch, reuse | Same transaction and shared fact; same ID agrees with GetMatchProposal |
| GetMatchProposal | Current read authorization and shared fact |
| Apply initial/conflict receipt | Current authorized transaction and shared fact, including ordinary manual partial application |
| Apply completed receipt | Latest transaction-local accepted fact after consumption |
| Existing receipt replay | Immutable receipt; current persisted adjudication authorization, not caller-controlled input; no terminal-state precheck before successful replay |
| Web history | Project both status and accepted, independently of success/conflict outcome |

## State and actor matrix

- Unaccepted pending/new/no-op/locked: do not manufacture acceptance.
- Manual `fill_empty` partial and MCP partial: pending plus accepted=true even when manual application leaves `applied_at` unset.
- Reused pending after either partial actor: accepted=true; fields are not consumed or applied twice.
- Supplement-and-close: terminal closure distinguishes accepted content from discarded remainder.
- Pure rejection: rejected plus accepted=false.
- Manual rejection after either partial actor: rejected plus accepted=true; display accepted content with rejected remainder, never all rejected; content remains applied.
- Terminal new request: honest conflict without metadata/field/provenance half-commit; prior accepted fact survives.
- Exact replay, altered payload, different credential: preserve immutable replay, reject digest mismatch, and enforce credential namespace respectively.
- Read/metadata-write/library/user revocation: enforce current authority before returning adjudication receipts; restored authority permits the original immutable replay.
- Cleanup/reopen: retain minimal facts and adjudication authority markers; material loss means Undo unavailable, not fabricated restoration or proposal reopening.

## Regression pointers

`internal/bootstrap/mcp_adjudication_test.go`: `verifyProposalAdjudication`, `verifyManualPartialAdjudication`, `verifyAcceptedMatchReuse`, `verifyAdjudicationReceiptAuthority`, `verifyAdjudicationRaces`, `verifyAdjudicationFaults`, `verifyAdjudicationAuthors`.

The production author/enrichment hosts exercise actual HTTP and compiled stdio. `web/src/pages/settings/metadataOperations.test.tsx` covers the display mapping and pending-cleanup remount recovery. Digest compatibility and existing proposal transaction rollback guards remain required, not replaced by this checklist.

## Verification budget

The latest reuse repair ran real red/green, targeted two rounds, eight relevant packages, one successful complete backend (38 packages), history-page tests, lint and generated-contract checks. UI-only preceding repair ran full frontend121 files/1164 tests, build and lint. Review the matrix as one independent pass; rerun affected regressions only unless another change alters wider integration behavior. Final14 owns the final combined gate. Historical Windows rename errors remain recorded without an unproved root-cause claim.
