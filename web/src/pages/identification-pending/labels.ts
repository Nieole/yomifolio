import type { Escalation, SeriesIdentification } from '../../api/generated';

type Translate = (key: string, params?: Record<string, string | number>, defaultValue?: string) => string;

// 分类没有词条时（比如之后新增的分类）退到它所属的类别名，原始分类键照样露出来。
export function categoryLabel(t: Translate, category: string, escalationClass: string) {
  const fallback = `${t(`identification.class.${escalationClass}`, undefined, escalationClass)} · ${category}`;
  return t(`identification.category.${category}`, undefined, fallback);
}

// 与服务端的待人工判据一致：备注总在等人；识别类与复核类上报只在系列待人工或待复核时等人，
// 已审定的系列留着它们但不等。
export function waitingEscalations(identification: SeriesIdentification): Escalation[] {
  const deciding = identification.status === 'awaiting_owner' || identification.status === 'needs_recheck';
  return (identification.escalations ?? []).filter((e) => e.class === 'note' || deciding);
}
