/**
 * 本文件守卫站内「MCP 接入配置指南」的构建期转换：docs/ 下两份 markdown 必须被转成
 * 可直接注入弹层的 HTML，且真实文档里不能残留未转换的标题标记。
 */

import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { markdownToHtml, renderMcpDocs } from './mcpDocs';

const fixture = `# Title

Paragraph with **bold**, \`inline\` and a [link](https://example.com).

- one
- two

\`\`\`json
{"mcpServers":{}}
\`\`\`

| a | b |
| - | - |
| 1 | 2 |
`;

describe('markdownToHtml', () => {
  it('converts headings, emphasis, inline code, links, lists, fenced code and tables', () => {
    const html = markdownToHtml(fixture);
    expect(html).toContain('<h1>Title</h1>');
    expect(html).toContain('<strong>bold</strong>');
    expect(html).toContain('<code>inline</code>');
    expect(html).toContain('href="https://example.com"');
    expect(html).toContain('<li>one</li>');
    expect(html).toContain('<pre><code class="language-json">');
    expect(html).toContain('<table>');
    expect(html).toContain('<td>1</td>');
  });
});

describe('renderMcpDocs', () => {
  it('renders both locales of the tracked guide from the repository root', () => {
    const docs = renderMcpDocs(resolve(import.meta.dirname, '../..'));
    expect(Object.keys(docs).sort()).toEqual(['en-US', 'zh-CN']);
    for (const html of Object.values(docs)) {
      expect(html).toContain('<h1');
      // 真实文档有多个二级标题；一个都没有说明整段被当成纯文本漏转了。
      expect(html.match(/<h2/g)?.length ?? 0).toBeGreaterThan(3);
      expect(html.length).toBeGreaterThan(2000);
    }
    expect(docs['zh-CN']).toContain('客户端配置');
  });
});
