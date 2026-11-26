/**
 * 能记为系列识别的目录型来源：键与显示名在前端只此一份，与服务端的来源目录（metadata 的 Catalogue）对齐。
 * LLM 的回答不指向任何来源条目，不在其中。
 */

const SOURCE_LABELS: Record<string, string> = {
  bangumi: 'Bangumi',
  anilist: 'AniList',
  mangadex: 'MangaDex',
  myanimelist: 'MyAnimeList',
  comicvine: 'Comic Vine',
};

/** 能记为系列识别的来源键，按默认优先级排列。 */
export const IDENTIFICATION_SOURCES = Object.keys(SOURCE_LABELS);

export function isIdentificationSource(provider: string): boolean {
  return IDENTIFICATION_SOURCES.includes(provider);
}

/** 来源的显示名；不认识的来源原样显示它的键。 */
export function sourceLabel(source: string): string {
  return SOURCE_LABELS[source] ?? source;
}
