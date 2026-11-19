import json, math, pathlib, statistics
root=pathlib.Path('C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05/.scratch/search-experience/acceptance')
def load(label):
 return json.loads((root/(label+'.json')).read_text(encoding='utf-8'))['samples']
def percentile(s,p):
 a=sorted(s['nanoseconds']); assert len(a)==100
 return a[math.ceil(p*len(a))-1]/1e6
old={(s['query'],s['target']):percentile(s,.95) for s in load('baseline')}
now={(s['query'],s['target']):percentile(s,.95) for s in load('baseline-finalperf05')}
lines=['# finalperf05 全部采样（失败不删）','','10预热+100请求；p50/p95最近秩。旧版同期仍用原固定archive二进制。门禁列同时列原旧版/同期旧版；正式深页预算相对同轮首批。宽松查询独立报告。','']
for label in ['candidate-finalperf05','candidate-finalperf05-repeat']:
 samples=load(label); first={(s['query'],s['target']):percentile(s,.95) for s in samples if s['phase']=='first'}
 lines+=['## '+label,'','|查询|范围|页|p50 ms|p95 ms|/原旧版|/同期旧版|/本轮首批|门禁(原/同期)|','|---|---|---|---:|---:|---:|---:|---:|---|']
 for s in samples:
  k=(s['query'],s['target']); v=percentile(s,.95); historical=old.get(k); concurrent=now.get(k)
  a=v/historical if historical and s['phase']=='first' else None;b=v/concurrent if concurrent and s['phase']=='first' else None;d=v/first[k] if s['phase']!='first' else None
  fmt=lambda x:'—' if x is None else f'{x:.4f}'
  result=('PASS' if d<=2 else 'FAIL') if d is not None else ('独立' if a is None else ('PASS' if a<=2 else 'FAIL')+'/'+('PASS' if b<=2 else 'FAIL'))
  lines.append(f"|{s['query']}|{s['target']}|{s['phase']}|{percentile(s,.5):.3f}|{v:.3f}|{fmt(a)}|{fmt(b)}|{fmt(d)}|{result}|")
 lines+=['']
lines+=['## 同期固定旧版首批','','|查询|范围|p50 ms|p95 ms|','|---|---|---:|---:|']
for s in load('baseline-finalperf05'):lines.append(f"|{s['query']}|{s['target']}|{percentile(s,.5):.3f}|{percentile(s,.95):.3f}|")
(root/'measurements-finalperf05.md').write_text('\n'.join(lines)+'\n',encoding='utf-8')
rows=[json.loads(s) for s in (root/'load-finalperf05.jsonl').read_text(encoding='utf-8-sig').splitlines()]
cpu=[r['cpu'] for r in rows];procs={}
for r in rows:
 for p in r['processes']:
  if p['Name'].startswith(('python','chdman')):procs.setdefault(p['Name'].split('#')[0],[]).append(p['PercentProcessorTime'])
summary={'samples':len(rows),'start':rows[0]['time'],'end':rows[-1]['time'],'system_cpu_min':min(cpu),'system_cpu_median':statistics.median(cpu),'system_cpu_max':max(cpu),'system_cpu_100_count':sum(v==100 for v in cpu),'external_observed':{k:{'samples':len(v),'min':min(v),'median':statistics.median(v),'max':max(v)} for k,v in procs.items()}}
(root/'load-finalperf05-summary.json').write_text(json.dumps(summary,indent=2)+'\n',encoding='utf-8')
print(json.dumps(summary,indent=2))
