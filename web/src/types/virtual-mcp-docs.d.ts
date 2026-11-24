// 构建期由 web/build/mcpDocs.ts 注入的虚拟模块（vite 插件 mcp-docs），内容是按 locale 索引的文档 HTML。
declare module 'virtual:mcp-docs' {
  export const mcpDocs: Record<string, string>;
}
