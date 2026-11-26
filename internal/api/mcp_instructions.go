package api

import (
	"strings"

	"yomifolio/internal/application/delegated"
)

// mcpMainLoop is how an agent that may claim works through the library. It names every main-loop
// tool, so none of them needs a line of its own.
const mcpMainLoop = `Your job is to identify each series of the library: which source catalogue entry it is.
Main loop, repeat it until a claim says to stop:
1. library_claim_series hands you up to 20 series, each with an evidence packet: local clues and source candidates with match signals.
2. Decide every series from its packet, then answer them all in one library_submit_verdicts call: identify (cite candidate_ids), not_found, or escalate with a category when you cannot tell. Each verdict needs a one-sentence reason.
3. When no candidate fits a series, library_research_series searches the sources again with your own term; then decide it as above.
An empty claim says why: empty_reason all_processed means stop, any other reason means retry later. library_identification_progress counts the series in each status.
Your credential is the owner's permission: write directly and do not ask a human first. Identifying fills empty metadata, and every write leaves a receipt you can undo.`

// mainLoopTools are the tools mcpMainLoop explains.
var mainLoopTools = []string{"library_claim_series", "library_submit_verdicts", "library_research_series", "library_identification_progress"}

// toolPurposes gives every other tool one sentence, in the order the instructions list them. A
// tool the listing shows needs one here; library_identification_progress has one for credentials
// that cannot run the main loop. withWrite, when set, replaces purpose for a credential that also
// holds metadata_write, which lets the tool do more.
var toolPurposes = []struct{ name, purpose, withWrite string }{
	{"library_identification_progress", "count the series in each identification status.", ""},
	{"library_find_series", "find series by library, ID, date added or missing metadata; series IDs come from here.", ""},
	{"library_get_series", "read one series' name, title and directory name.", ""},
	{"library_list_books", "list the books of one series.", ""},
	{"library_get_book_clues", "read the ComicInfo identity fields of one book.", ""},
	{"library_get_series_cover", "look at a series' current cover.", ""},
	{"library_get_page_thumbnails", "look at chosen pages of one book.", ""},
	{"library_get_identification_images", "compare a series' cover and pages with its candidates' covers by eye.", ""},
	{"library_replace_identification", "replace your own wrong identification with another candidate from the series' evidence.", ""},
	{"library_correct_series_field", "correct a title, summary, publisher, status or rating a source got wrong, if nobody changed it since.", ""},
	{"library_get_series_authors", "read a series' authors and the credits of its identified sources before correcting them.", ""},
	{"library_correct_authors", "replace a wrong author of a series with a credit of its identified source.", ""},
	{"library_improve_cover", "replace a series' automatic cover with a better page of its own books.", ""},
	{"library_add_series_relation", "record a franchise relation, such as a sequel, between two series.", ""},
	{"library_find_duplicate_series", "find series that look like duplicates of each other.", ""},
	{"library_count_tags", "count the series carrying each tag, to spot tag problems.", ""},
	{"library_report_series", "tell the owner about a series: a directory grouping, duplicate, tag or protected-value problem.",
		"tell the owner about a series: a directory grouping, duplicate, tag or protected-value problem, or an identification you doubt, a person's or yours, which goes to recheck."},
	{"library_get_metadata_operation", "read one write receipt.", ""},
	{"library_list_metadata_operations", "list the write receipts of one series.", ""},
	{"library_undo_metadata_operation", "undo one write receipt of an agent; the owner's own are refused.", ""},
	{"library_start_scan", "scan a library for new and changed files.", ""},
	{"library_start_metadata_cleanup", "discard the undo material of several receipts in one run.", ""},
	{"library_get_metadata_cleanup", "read the result of a cleanup.", ""},
	{"library_cleanup_metadata_operation", "discard the undo material of one receipt; it can no longer be undone.", ""},
	{"library_get_run", "read the status of a run you started.", ""},
	{"library_wait_run", "wait for a run you started to finish.", ""},
	{"library_list_runs", "list the runs you started in one library.", ""},
	{"library_cancel_run", "stop a run you started.", ""},
}

// mcpInstructions is what initialization tells an agent: the main loop when its credential may
// claim, then one line for each other tool its listing shows.
func mcpInstructions(tools grantedTools) string {
	var b strings.Builder
	b.WriteString("Yomifolio comic library tools.\n")
	explained := map[string]bool{}
	if tools.allows("library_claim_series") {
		b.WriteString(mcpMainLoop)
		b.WriteString("\n")
		for _, name := range mainLoopTools {
			explained[name] = true
		}
	}
	var others []string
	for _, tool := range toolPurposes {
		if tools.allows(tool.name) && !explained[tool.name] {
			purpose := tool.purpose
			if tool.withWrite != "" && tools[delegated.MetadataWrite] {
				purpose = tool.withWrite
			}
			others = append(others, "- "+tool.name+": "+purpose)
		}
	}
	if len(others) > 0 {
		b.WriteString("Other tools:\n" + strings.Join(others, "\n"))
	}
	return strings.TrimSuffix(b.String(), "\n")
}
