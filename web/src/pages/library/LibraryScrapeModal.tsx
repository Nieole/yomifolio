import type { ScrapeProvider } from '../../hooks/useScrapeProviders';
import { SeriesSearchModal } from '../series-detail/SeriesSearchModal';
import type { Series as DetailSeries } from '../series-detail/types';

import type { Series } from './types';
import type { useSeriesScraping } from './hooks/useSeriesScraping';

type ScrapingState = ReturnType<typeof useSeriesScraping>;

interface LibraryScrapeModalProps {
  scraping: ScrapingState;
  providers: ScrapeProvider[];
}

export function LibraryScrapeModal({ scraping, providers }: LibraryScrapeModalProps) {
  if (!scraping.scrapingSeries) return null;

  return (
    <SeriesSearchModal
      open={scraping.showScrapeModal}
      onClose={scraping.closeScrapeModal}
      providerLabel={providers.find((p) => p.id === scraping.scrapeProvider)?.name ?? scraping.scrapeProvider}
      provider={scraping.scrapeProvider}
      identification={scraping.scrapeIdentification}
      modalSearchQuery={scraping.scrapeModalSearchQuery}
      isScraping={scraping.isScraping}
      searchResults={scraping.scrapeSearchResults}
      currentOffset={scraping.scrapeOffset}
      searchTotal={scraping.scrapeTotal}
      currentSeries={scraping.scrapeSeriesDetail || (scraping.scrapingSeries as unknown as DetailSeries)}
      currentTags={scraping.scrapeCurrentTags}
      lockedFields={scraping.scrapeLockedFields}
      selectedResult={scraping.selectedScrapeResult}
      onSearchQueryChange={scraping.setScrapeModalSearchQuery}
      onReSearch={scraping.reSearch}
      onSelectMetadata={scraping.setSelectedScrapeResult}
      onApplyMetadata={(metadata) => scraping.applyScrape(metadata as unknown as Record<string, unknown>)}
    />
  );
}

export type { Series };
