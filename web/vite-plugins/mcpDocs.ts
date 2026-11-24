/**
 * 业务说明：把 docs/ 下的 MCP 接入指南在构建期转成 HTML，注入虚拟模块 `virtual:mcp-docs`，
 * 供设置页的只读说明弹层使用。
 * 运行时因此不解析 markdown，文档正文也不必抄进 locale 文件；换文档只改 DOC_FILES。
 * markdown 是仓库自己维护的文件，属于可信内容，注入时不再做二次净化。
 */

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { Marked } from 'marked';
import type { Plugin } from 'vite';

export const MCP_DOCS_MODULE = 'virtual:mcp-docs';

const RESOLVED_ID = `\0${MCP_DOCS_MODULE}`;

// locale → 仓库内的文档路径。前端找不到当前语言时回落到 en-US。
const DOC_FILES: Record<string, string> = {
  'zh-CN': 'docs/mcp.zh-CN.md',
  'en-US': 'docs/mcp.md',
};

const marked = new Marked({ gfm: true });

export function markdownToHtml(source: string): string {
  return marked.parse(source) as string;
}

export function renderMcpDocs(repositoryRoot: string): Record<string, string> {
  const docs: Record<string, string> = {};
  for (const [locale, path] of Object.entries(DOC_FILES)) {
    docs[locale] = markdownToHtml(readFileSync(resolve(repositoryRoot, path), 'utf8'));
  }
  return docs;
}

export function mcpDocsPlugin(): Plugin {
  // vite 的 root 是 web/，文档在仓库根。
  let repositoryRoot = resolve(process.cwd(), '..');
  return {
    name: 'mcp-docs',
    configResolved(config) {
      repositoryRoot = resolve(config.root, '..');
    },
    resolveId(id) {
      return id === MCP_DOCS_MODULE ? RESOLVED_ID : null;
    },
    load(id) {
      if (id !== RESOLVED_ID) return null;
      return `export const mcpDocs = ${JSON.stringify(renderMcpDocs(repositoryRoot))}`;
    },
  };
}
