/**
 * 业务说明：MCP 接入指南与服务器配置共用的地址换算——拼出客户端要填的 `/mcp` 地址与两段可直接粘贴的
 * 客户端样例。公开地址的规范形式（去末尾斜杠）在保存前由分区 payload 归一，这里只负责拼接时兜底。
 * 单独成文件是为了让组件文件只导出组件（react-refresh 约束）。
 */

// mcpEndpoint 拼出客户端地址：/mcp 是服务端固定挂载点，用户只填 scheme+host。
export function mcpEndpoint(publicUrl: string): string {
  return `${publicUrl.replace(/\/+$/, '')}/mcp`;
}

// 客户端样例用占位符而不是真密钥：页面上拿不到别人的密钥，也不该在 DOM 里留真密钥。
export function httpSample(endpoint: string): string {
  return JSON.stringify({
    mcpServers: {
      yomifolio: { type: 'http', url: endpoint, headers: { Authorization: 'Bearer <issued-secret>' } },
    },
  }, null, 2);
}

export function stdioSample(publicUrl: string): string {
  return JSON.stringify({
    mcpServers: {
      yomifolio: {
        command: '/opt/yomifolio/mcp-proxy',
        args: ['--url', publicUrl.replace(/\/+$/, '')],
        env: { YOMIFOLIO_MCP_TOKEN: '<issued-secret>' },
      },
    },
  }, null, 2);
}
