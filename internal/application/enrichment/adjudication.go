package enrichment

// AdjudicationResult is how an operation decided an existing proposal, kept on its receipt; undo
// reclaims the field provenance it linked to that proposal. No current command produces it.
type AdjudicationResult struct {
	Accepted   bool   `json:"accepted"`
	ProposalID int64  `json:"proposal_id"`
	Mode       string `json:"mode"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
}
