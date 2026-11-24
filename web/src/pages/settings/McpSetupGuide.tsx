/**
 * 业务说明：设置页顶部的「接入配置指南」——把客户端真正需要的三件事（地址、凭据、配置样例）
 * 摊在页面上，用户不必再去翻仓库里的文档；完整说明由 McpDocsDialog 按需打开。
 * 折叠状态记在 localStorage：多数人只配一次，之后这卡片就该让位给凭据列表。
 */

import { useState } from 'react';
import { ChevronDown, ChevronUp, Copy, FileText } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { httpSample, mcpEndpoint, stdioSample } from './mcpEndpoint';
import { sectionClassName } from './shared';

const COLLAPSED_KEY = 'mcp-guide-collapsed';
const monoClass = 'block overflow-x-auto rounded-xl border border-gray-800 bg-komgaBackground/80 p-3 text-xs leading-5 text-gray-200';

export function McpSetupGuide({ publicUrl, onOpenDocs }: { publicUrl: string; onOpenDocs: () => void }) {
  const { t } = useI18n();
  const [collapsed, setCollapsed] = useState(() => window.localStorage.getItem(COLLAPSED_KEY) === '1');
  const [copied, setCopied] = useState(false);
  const endpoint = mcpEndpoint(publicUrl);

  const toggle = () => {
    const next = !collapsed;
    window.localStorage.setItem(COLLAPSED_KEY, next ? '1' : '0');
    setCollapsed(next);
  };
  const copy = () => {
    void navigator.clipboard?.writeText(endpoint).then(() => setCopied(true)).catch(() => setCopied(false));
  };

  return <section className={sectionClassName}>
    <div className="flex flex-wrap items-start justify-between gap-3">
      <h3 className="text-lg font-semibold">{t('mcp.guide.title')}</h3>
      <div className="flex flex-wrap gap-2">
        <button type="button" onClick={onOpenDocs} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 px-3 py-2 text-sm text-white hover:bg-gray-800">
          <FileText className="h-4 w-4" />{t('mcp.docs.open')}
        </button>
        <button type="button" onClick={toggle} aria-expanded={!collapsed} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 px-3 py-2 text-sm text-white hover:bg-gray-800">
          {collapsed ? <ChevronDown className="h-4 w-4" /> : <ChevronUp className="h-4 w-4" />}
          {collapsed ? t('mcp.guide.expand') : t('mcp.guide.collapse')}
        </button>
      </div>
    </div>
    {!collapsed && <div className="mt-4 space-y-4">
      <div className="space-y-2">
        <p className="text-sm font-medium text-gray-300">{t('mcp.guide.endpoint')}</p>
        {publicUrl
          ? <div className="flex flex-wrap items-center gap-2">
            <code className="break-all rounded-lg border border-gray-800 bg-komgaBackground/80 px-3 py-2 text-sm text-amber-200">{endpoint}</code>
            <button type="button" onClick={copy} className="inline-flex items-center gap-2 rounded-lg border border-gray-700 px-3 py-2 text-sm text-white hover:bg-gray-800">
              <Copy className="h-4 w-4" />{copied ? t('mcp.guide.copied') : t('mcp.guide.copy')}
            </button>
          </div>
          : <p className="text-sm text-amber-200">{t('mcp.guide.endpointMissing')}</p>}
      </div>
      <p className="text-sm text-gray-400">{t('mcp.guide.issueHint')}</p>
      <div className="space-y-2">
        <p className="text-sm font-medium text-gray-300">{t('mcp.guide.http')}</p>
        <pre className={monoClass}><code>{httpSample(endpoint)}</code></pre>
      </div>
      <div className="space-y-2">
        <p className="text-sm font-medium text-gray-300">{t('mcp.guide.stdio')}</p>
        <pre className={monoClass}><code>{stdioSample(publicUrl || 'https://library.example.com')}</code></pre>
        <p className="text-sm text-gray-400">{t('mcp.guide.stdioHint')}</p>
      </div>
    </div>}
  </section>;
}
