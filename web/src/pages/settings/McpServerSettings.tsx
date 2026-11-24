/**
 * 业务说明：设置页里的 MCP 服务器配置卡——把 `mcp.public_url` 与 `mcp.allow_lan_http`
 * 从配置文件搬到界面上，避免「签凭据在界面、改地址回配置文件」的割裂。
 * 保存走与其它设置分区相同的分区快照与版本前置条件（If-Match），只提交 mcp 段。
 */

import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from '../../features/settings-editor';
import { normalizePublicUrl } from './mcpEndpoint';
import { FieldErrors, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function McpServerSettings() {
  const { t } = useI18n();
  const { config, setConfig, fieldErrors, isSectionSaving, saveConfig } = useSettings();
  if (!config) return null;
  const mcp = config.mcp;
  const saving = isSectionSaving('mcp');

  return <section className={sectionClassName}>
    <h3 className="text-lg font-semibold">{t('mcp.server.title')}</h3>
    <label className="block">
      {t('mcp.server.publicUrl')}
      <input
        className={inputClassName}
        value={mcp.public_url}
        placeholder="https://library.example.com"
        disabled={saving}
        onChange={(event) => {
          const publicUrl = normalizePublicUrl(event.target.value);
          setConfig((current) => current ? { ...current, mcp: { ...current.mcp, public_url: publicUrl } } : current);
        }}
      />
    </label>
    <FieldErrors messages={fieldErrors('mcp.public_url')} />
    <p className="text-sm text-gray-400">{t('mcp.server.publicUrlHint')}</p>
    <div className="space-y-1">
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={mcp.allow_lan_http}
          disabled={saving}
          onChange={(event) => {
            const allow = event.target.checked;
            setConfig((current) => current ? { ...current, mcp: { ...current.mcp, allow_lan_http: allow } } : current);
          }}
        />
        {t('mcp.server.allowLan')}
      </label>
      <p className="text-sm leading-6 text-amber-200">{t('mcp.server.allowLanRisk')}</p>
    </div>
    <SettingsSaveBar saving={saving} label={t('mcp.server.save')} hint={t('mcp.server.publicUrlHint')} onSave={() => saveConfig('mcp', t('mcp.saved'))} />
  </section>;
}
