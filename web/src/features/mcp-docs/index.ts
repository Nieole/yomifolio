/**
 * 业务说明：站内「MCP 接入配置指南」的正文来源。构建期由 vite 插件把 docs/ 下两份 markdown
 * 转成 HTML 放进虚拟模块，这里只做语言回落，供设置页的说明弹层渲染。
 */

import { mcpDocs } from 'virtual:mcp-docs';

// docHtml 按当前界面语言取文档；没有对应语言的译文时用英文原文，避免弹层空白。
export function docHtml(locale: string): string {
  return mcpDocs[locale] ?? mcpDocs['en-US'] ?? '';
}
