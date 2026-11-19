import json, math, pathlib, statistics, hashlib, subprocess, datetime
root=pathlib.Path('C:/Users/saber/.thinkrail/worktrees/manga-manager/search-acceptance-05')
a=root/'.scratch/search-experience/acceptance'
def load(label): return json.loads((a/(label+'.json')).read_text(encoding='utf-8'))['samples']
def pct(s,p=.95):
 n=sorted(s['nanoseconds']); assert len(n)==100
 return n[math.ceil(p*len(n))-1]/1e6
old={(s['query'],s['target']):pct(s) for s in load('baseline')}
now={(s['query'],s['target']):pct(s) for s in load('baseline-idle-final05')}
lines=['# idle-final05 全部测量（历史及本轮失败全部保留）','','每场景10预热+100次；p50/p95最近秩。正式首批比较使用同负载固定archive重测；原历史基线仅作参考，未替换或删除。深页比较对应同轮首批。','']
summary={}
for label in ['candidate-idle-final05','candidate-idle-final05-repeat','candidate-idle-final05-confirm']:
 samples=load(label); first={(s['query'],s['target']):pct(s) for s in samples if s['phase']=='first'}
 lines+=['## '+label,'','|查询|范围|页|p50 ms|p95 ms|/历史旧版(参考)|/同期旧版|/本轮首批|正式门禁|','|---|---|---|---:|---:|---:|---:|---:|---|']
 ratios=[];fail=[]
 for s in samples:
  k=(s['query'],s['target']); v=pct(s);h=old.get(k);c=now.get(k)
  hr=v/h if h and s['phase']=='first' else None;cr=v/c if c and s['phase']=='first' else None;dr=v/first[k] if s['phase']!='first' else None
  ratio=dr if dr is not None else cr
  if ratio is not None:
   ratios.append(ratio)
   if ratio>2:fail.append({'query':s['query'],'target':s['target'],'phase':s['phase'],'p95_ms':v,'ratio':ratio})
  fmt=lambda x:'—' if x is None else f'{x:.4f}'
  lines.append(f"|{s['query']}|{s['target']}|{s['phase']}|{pct(s,.5):.3f}|{v:.3f}|{fmt(hr)}|{fmt(cr)}|{fmt(dr)}|{'独立' if ratio is None else 'PASS' if ratio<=2 else 'FAIL'}|")
 summary[label]={'scenarios':len(samples),'max_ratio':max(ratios),'failures':fail}
 lines+=['']
lines+=['## 同负载固定旧archive完整首批','','|查询|范围|p50 ms|p95 ms|','|---|---|---:|---:|']
for s in load('baseline-idle-final05'):lines.append(f"|{s['query']}|{s['target']}|{pct(s,.5):.3f}|{pct(s):.3f}|")
(a/'measurements-idle-final05.md').write_text('\n'.join(lines)+'\n',encoding='utf-8')
rows=[json.loads(s) for file in ['load-idle-final05.jsonl','load-idle-final05-confirm.jsonl'] for s in (a/file).read_text(encoding='utf-8-sig').splitlines()]
cpu=[r['cpu'] for r in rows]
summary['load']={'samples':len(rows),'start':rows[0]['time'],'end':rows[-1]['time'],'cpu_min':min(cpu),'cpu_median':statistics.median(cpu),'cpu_p95':sorted(cpu)[math.ceil(.95*len(cpu))-1],'cpu_max':max(cpu),'cpu_100_count':cpu.count(100),'freeKB_min':min(r['freeKB'] for r in rows)}
(a/'summary-idle-final05.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print(json.dumps(summary,ensure_ascii=False,indent=2))
# Prove reuse of exact prior source and binaries (all recorded files, not only handpicked production files).
checks=[];current=[]
for line in (a/'finalperf05-source.sha256').read_text(encoding='utf-8-sig').splitlines():
 expected,path=line.split('  ',1);p=pathlib.Path(path)
 actual=hashlib.sha256(p.read_bytes()).hexdigest()
 checks.append({'path':path,'matches':expected==actual});current.append(actual+'  '+path)
(a/'idle-final05-source.sha256').write_text('\n'.join(current)+'\n',encoding='utf-8')
(a/'idle-final05-source-verification.json').write_text(json.dumps({'files':len(checks),'all_match':all(x['matches'] for x in checks),'mismatches':[x for x in checks if not x['matches']]},indent=2)+'\n',encoding='utf-8')
print('Source verification:',len(checks),all(x['matches'] for x in checks))
# Approximate run boundaries from log end mtime minus driver-reported wall duration.
windows={}
for label,duration in [('baseline-idle-final05',370.95),('candidate-idle-final05',446.84),('candidate-idle-final05-repeat',347.20),('candidate-idle-final05-confirm',424.44)]:
 end=datetime.datetime.fromtimestamp((a/(label+'.log')).stat().st_mtime,datetime.timezone.utc)
 start=end-datetime.timedelta(seconds=duration)
 rs=[x for x in rows if start<=datetime.datetime.fromisoformat(x['time'].replace('Z','+00:00'))<=end]
 cs=sorted(x['cpu'] for x in rs)
 windows[label]={'start_utc':start.isoformat(),'end_utc':end.isoformat(),'duration_seconds':duration,'samples':len(rs),'cpu_min':min(cs),'cpu_median':statistics.median(cs),'cpu_p95':cs[math.ceil(len(cs)*.95)-1],'cpu_max':max(cs),'external_chdman':[x for x in rs if any(y['Name'].startswith('chdman') for y in x['processes'])]}
(a/'load-idle-final05-windows.json').write_text(json.dumps(windows,indent=2)+'\n',encoding='utf-8')
