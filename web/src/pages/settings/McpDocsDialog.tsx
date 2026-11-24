/**
 * 业务说明：只读打开仓库里的 MCP 接入指南全文。正文由构建期把 docs/mcp*.md 转成 HTML
 * 注入虚拟模块（见 web/build/mcpDocs.ts），这里只按界面语言取一份并塞进弹层，
 * 因此文档与仓库里的 markdown 不会各写一遍、互相漂移。
 */

import { useI18n } from '../../i18n/LocaleProvider';
import { ModalShell } from '../../components/ui/ModalShell';
import { docHtml } from '../../features/mcp-docs';

export function McpDocsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t, locale } = useI18n();
  return <ModalShell open={open} onClose={onClose} title={t('mcp.docs.title')} size="wide">
    <div className="mcp-doc" dangerouslySetInnerHTML={{ __html: docHtml(locale) }} />
  </ModalShell>;
}
