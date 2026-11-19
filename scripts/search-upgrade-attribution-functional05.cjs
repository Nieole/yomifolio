// Independent archive, with current uncommitted database production changes
// virtually overlaid. Timing instrumentation exists only in the external copy.
const fs=require('node:fs');
const root='C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05';
const art='C:/Users/saber/.thinkrail/search-experience-research/acceptance-05';
let source=fs.readFileSync(root+'/internal/database/migrate.go','utf8');
for(const kind of ['Series','Book']) {
 const call=`if err := rebuild${kind}SearchIndex(db); err != nil {\n\t\t\treturn err\n\t\t}`;
 if(!source.includes(call))throw Error('instrumentation anchor absent '+kind);
 source=source.replace(call,`searchStart := time.Now()\n\t\t${call}\n\t\tfmt.Printf("SEARCH_BACKFILL ${kind} user_version=%d elapsed_ns=%d\\n", userVersion, time.Since(searchStart).Nanoseconds())`);
}
fs.writeFileSync(art+'/functional05-migrate-timed.go',source);
const Replace={};
for(const file of ['series_search.go','search_rank.go','search_archive_acceptance_test.go'])Replace[art+'/final-functional05/internal/database/'+file]=root+'/internal/database/'+file;
Replace[art+'/final-functional05/internal/database/migrate.go']=art+'/functional05-migrate-timed.go';
fs.writeFileSync(art+'/functional05-final-overlay.json',JSON.stringify({Replace},null,2));
