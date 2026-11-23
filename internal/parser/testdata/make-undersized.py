from pathlib import Path
import zlib

def uv(n):
 b=bytearray()
 while n>127: b.append((n&127)|128); n>>=7
 b.append(n); return b

def get(b,p):
 v=0;s=0;start=p
 while True:
  x=b[p];p+=1;v|=(x&127)<<s;s+=7
  if x<128:return v,p

# Run from repository root after generating .tmp/solidfix/solid.cbr as documented.
src=Path('.tmp/solidfix/solid.cbr').read_bytes(); out=bytearray(src[:8]); p=8; first=True
while p<len(src):
 size,q=get(src,p+4); end=q+size; h=src[q:end]; kind,i=get(h,0);flags,i=get(h,i);extra=0;packed=0
 if flags&1:extra,i=get(h,i)
 if flags&2:packed,i=get(h,i)
 if kind==2 and first:
  f,j=get(h,i);usize,k=get(h,j);attrs,l=get(h,k); tail=l+(4 if f&2 else 0); crcend=tail+(4 if f&4 else 0)
  comp,_=get(h,crcend)
  print('global solid archive, first file comp=',comp,'first-file-solid=',bool(comp&64),'original size=',usize,'packed=',packed)
  h=h[:i]+uv(f&~4)+uv(1)+h[k:tail]+h[crcend:];first=False
 hh=uv(len(h))+h;out+=zlib.crc32(hh).to_bytes(4,'little')+hh+src[end:end+packed];p=end+packed
Path('internal/parser/testdata/solid-undersized.cbr').write_bytes(out)
Path('internal/parser/testdata/solid-compressed.cbr').write_bytes(src)
