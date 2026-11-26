/**
 * 本文件由 cmd/tsgen 自动生成，请勿手工编辑。
 * 它以 Go 后端的响应结构体为单一事实源生成前端契约类型，防止手写类型与后端漂移。
 * 重新生成：`go run ./cmd/tsgen`；CI 会校验其与源一致。
 */

export interface Operation {
  undo_unavailable?: boolean;
  cleaned_by?: string;
  adjudication?: AdjudicationResult;
  identification?: IdentificationBasis;
  cover_selection?: CoverSelection;
  relation?: RelationChange;
  old_cover_material?: string;
  new_cover_material?: string;
  version: number;
  id: string;
  request_key: string;
  actor: Subject;
  series_id: number;
  library_id: number;
  kind: string;
  parent_id?: string;
  created_at: string;
  outcome: string;
  reason: string;
  before: State;
  after: State;
  fields: FieldAudit[];
  undone_by?: string;
  applied: boolean;
  replayed: boolean;
}

export interface AdjudicationResult {
  accepted: boolean;
  proposal_id: number;
  mode: string;
  reason: string;
  status: string;
}

export interface IdentificationBasis {
  entries: IdentifiedEntry[];
  placeholder_title?: boolean;
  superseded_proposals: number[];
}

export interface IdentifiedEntry {
  candidate_id: string;
  entry_url: string;
}

export interface CleanupResult {
  retry_request_key?: string;
  id: string;
  operation_id: string;
  actor: Subject;
  created_at: string;
  outcome: string;
  undo_unavailable: boolean;
  materials: MaterialResult[];
  replayed: boolean;
}

export interface MaterialResult {
  key: string;
  outcome: string;
}

export interface CoverSelection {
  book_id: number;
  page: number;
  quality_compared: boolean;
  reason: string;
}

export interface RelationChange {
  id: number;
  source_series_id: number;
  target_series_id: number;
  relation_type: string;
}

export interface CoverState {
  book_id: number;
  path: string;
  locked: boolean;
  automatic: boolean;
}

export interface Page {
  operations: Operation[];
  next_before?: number;
}

export interface State {
  authors: Author[];
  cover?: CoverState;
  title: string | null;
  summary: string | null;
  publisher: string | null;
  status: string | null;
  rating: number | null;
  tags: string[];
  links: Link[];
}

export interface FieldAudit {
  field: string;
  changed: boolean;
  reason: string;
  sources: FieldEvidence[];
  formula?: string;
}

export interface Subject {
  user_id: number;
  credential_id: string;
}

export interface FieldEvidence {
  candidate_id: string;
  source: string;
  metadata: SourceFacts;
}

export interface SourceFacts {
  Title: string;
  OriginalTitle: string;
  Summary: string;
  Publisher: string;
  Status: string;
  CoverURL: string;
  SiteRating?: SiteRating;
  Rating: number;
  Tags: string[];
  Authors: SeriesAuthor[];
  SourceID: number;
  SourceURL: string;
  Provider: string;
  Confidence: number;
  ReleaseDate: string;
  VolumeCount: number;
}

export interface SiteRating {
  value: number;
  scale: number;
}

export interface SeriesAuthor {
  Name: string;
  Role: string;
}

export interface Link {
  name: string;
  url: string;
}

export interface Author {
  name: string;
  role: string;
}

export interface Restrictions {
  user_id: number;
  name: string;
  library_ids: number[];
  capabilities: string[];
}

export interface Credential {
  id: string;
  user_id: number;
  name: string;
  library_ids: number[];
  capabilities: string[];
  revoked: boolean;
}

export interface IssuedCredential {
  id: string;
  user_id: number;
  name: string;
  library_ids: number[];
  capabilities: string[];
  revoked: boolean;
  token: string;
}

export interface SearchResult {
  hits: SearchHit[];
  total_hits: number;
  max_score: number;
  has_more: boolean;
  next_cursor: string;
}

export interface SearchHit {
  id: string;
  score: number;
  fields?: SearchHitFields;
}

export interface SearchHitFields {
  id: string;
  title: string;
  series_name: string;
  type: string;
  cover_path: string;
}

export interface RunLimits {
  scan_profile?: string;
  scanner_workers_configured?: number;
  scanner_workers_effective?: number;
  storage_profile?: string;
  volume_key?: string;
  scan_concurrency?: number;
  archive_open_concurrency?: number;
  cover_concurrency?: number;
  hash_concurrency?: number;
  pause_background_when_reading: boolean;
  idle_only_heavy_tasks: boolean;
  disable_same_disk_page_cache: boolean;
}

export interface RunSnapshot {
  run_id: number;
  task_id: number;
  type: string;
  scope: string;
  scope_id?: number;
  variant?: string;
  scope_name?: string;
  trigger?: string;
  status: string;
  message: string;
  message_code?: string;
  message_params?: Record<string, string>;
  error?: string;
  current: number;
  total: number;
  percent?: number;
  rate_per_minute?: number;
  eta_seconds?: number;
  can_cancel: boolean;
  can_pause: boolean;
  can_resume: boolean;
  retryable: boolean;
  paused_at?: string;
  pause_reason?: string;
  coalesced_count?: number;
  phase?: string;
  current_item?: string;
  effective_limit?: RunLimits;
  metrics?: Record<string, number>;
  labels?: Record<string, string>;
  params?: Record<string, string>;
  started_at?: string;
  updated_at: string;
  finished_at?: string;
}

export interface RunLiveSummary {
  active: number;
  queued: number;
  slots: number;
  paused: boolean;
  paused_all: boolean;
}

export interface RunLive {
  active: number;
  queued: number;
  slots: number;
  paused: boolean;
  paused_all: boolean;
  runs: RunSnapshot[];
}

export interface RunPush {
  sequence: number;
  prev: number;
  run?: RunSnapshot;
  live?: RunLiveSummary;
}

export interface TaskSummary {
  task_id: number;
  type: string;
  scope: string;
  scope_id?: number;
  variant?: string;
  scope_name?: string;
  disabled: boolean;
  fail_streak: number;
  last_success_at?: string;
  backoff_until?: string;
  stall_reason?: string;
  last_run?: RunSnapshot;
}

export interface RunEvent {
  at: string;
  kind: string;
  phase?: string;
  item?: string;
  reason?: string;
  action?: string;
  code?: string;
  detail?: string;
  count?: number;
}

export interface RunPhaseSpan {
  phase: string;
  started_at: string;
  duration_ms: number;
  current?: boolean;
}

export interface RunEventsResponse {
  run_id: number;
  events: RunEvent[];
  phases: RunPhaseSpan[];
  omitted_failures?: number;
  truncated?: boolean;
}

export interface RunSample {
  at: string;
  current: number;
  throughput_per_minute: number;
}

export interface RunSamplesResponse {
  run_id: number;
  samples: RunSample[];
  retention_days: number;
  expired?: boolean;
  truncated?: boolean;
}

export interface ValidationIssue {
  field: string;
  message: string;
  severity: string;
}

export interface ValidationResult {
  valid: boolean;
  issues: ValidationIssue[];
}

export interface FieldEffect {
  field: string;
  scope: string;
}

export interface Publication {
  saved: boolean;
  published: boolean;
  version: string;
  immediate: string[];
  next_operation: string[];
  restart_required: string[];
}

export interface SystemCapabilitiesResponse {
  supported_scan_formats: string[];
  supported_scan_profiles: string[];
  supported_log_levels: string[];
  supported_storage_profiles: string[];
  default_scan_formats: string;
  default_scan_interval: number;
  supported_llm_providers: string[];
  supported_llm_api_modes: string[];
}

export interface StorageFailureResponse {
  error: string;
  reason: string;
  library_id: number;
  library_name: string;
  library_path: string;
  path: string;
}

export interface RelocationDetail {
  ordinal: number;
  kind: string;
  record_type: string;
  record_id: number;
  source_path: string;
  target_path: string;
  reason: string;
  expected_size: number;
  actual_size: number;
  modified_ns: number;
  run_id?: number;
  run_status?: string;
  can_cancel: boolean;
  conflict_record_type?: string;
  conflict_record_id?: number;
  policy_before?: RelocationPolicy;
  policy_after?: RelocationPolicy;
}

export interface RelocationPolicy {
  storage_profile: string;
  io_policy: StorageIOPolicy;
}

export interface StorageIOPolicy {
  scan_concurrency: number;
  archive_open_concurrency: number;
  cover_concurrency: number;
  hash_concurrency: number;
  pause_background_when_reading: boolean;
  idle_only_heavy_tasks: boolean;
  disable_same_disk_page_cache: boolean;
}

export interface RelocationPlan {
  purpose?: string;
  protection_id?: number;
  id: number;
  library_id: number;
  library_name: string;
  library_root: string;
  source_root: string;
  target_root: string;
  run_id: number;
  operation_id?: number;
  commit_run_id?: number;
  status: string;
  series_count: number;
  book_count: number;
  blocker_count: number;
  anomaly_count: number;
  policy_count: number;
  anomaly_counts: Record<string, number>;
  samples: RelocationDetail[];
  created_at: string;
  updated_at: string;
  protection?: LibraryRelocationProtection;
}

export interface RelocationSourceRoots {
  library_root: string;
  records_outside_current_root: boolean;
  candidates: string[];
}

export interface RelocationDetails {
  items: RelocationDetail[];
  total: number;
  offset: number;
  limit: number;
}

export interface RelocationOperation {
  purpose?: string;
  protection_id?: number;
  scan_mode?: string;
  scan_interval?: number;
  scan_formats?: string;
  id: number;
  plan_id: number;
  library_id: number;
  run_id: number;
  status: string;
  source_root: string;
  target_root: string;
  series_count: number;
  book_count: number;
  code: string;
  updated_at: string;
  accept_anomalies: boolean;
  protected: boolean;
}

export interface LibraryRelocationProtection {
  library_id: number;
  operation_id: number;
  plan_id: number;
  facts_digest: string;
  anomaly_count: number;
  scan_mode: string;
  scan_interval: number;
  scan_formats: string;
}

export interface RelocationRestoration {
  protection?: LibraryRelocationProtection;
  accepted?: RelocationPlan;
  check?: RelocationPlan;
}

export interface SeriesIdentification {
  series_id: number;
  library_id: number;
  status: string;
  conclusions: Conclusion[];
  excluded_candidates: ExcludedCandidate[];
  not_found?: NotFoundVerdict;
  escalations?: Escalation[];
  owner_review?: OwnerReview;
  evidence?: EvidencePacket;
}

export interface Conclusion {
  source: string;
  entry_id: string;
  entry_url: string;
  title: string;
  original_title: string;
  aliases: string[];
  confirmer: Confirmer;
  confirmed_at: string;
  reason: string;
  proposal_id: number;
}

export interface Confirmer {
  kind: string;
  user_id: number;
  user_name: string;
  credential_id: string;
  credential_name: string;
}

export interface ExcludedCandidate {
  source: string;
  entry_id: string;
  entry_url: string;
  title: string;
  user_id: number;
  excluded_at: string;
  proposal_id: number;
}

export interface OwnerReview {
  user_id: number;
  user_name: string;
  reviewed_at: string;
}

export interface NotFoundVerdict {
  searched_sources: string[];
  confirmer: Confirmer;
  concluded_at: string;
  reason: string;
}

export interface Escalation {
  id: number;
  category: string;
  class: string;
  reason: string;
  raised_by: Confirmer;
  raised_at: string;
}

export interface EvidencePacket {
  fetched_at: string;
  local: EvidenceLocalClues;
  queries: string[];
  searches?: EvidenceSearch[];
  sources: EvidenceSource[];
  candidates: EvidenceCandidate[];
}

export interface EvidenceSearch {
  query: string;
  searched_at: string;
  added: string[];
  dropped?: number;
}

export interface EvidenceLocalClues {
  directory_name: string;
  book_count: number;
  file_names: string[];
  comic_info?: EvidenceComicInfo;
}

export interface EvidenceComicInfo {
  series?: string;
  authors?: string[];
  year?: number;
}

export interface EvidenceSource {
  source: string;
  status: string;
}

export interface EvidenceCandidate {
  id: string;
  source: string;
  entry_url: string;
  title: string;
  original_title?: string;
  authors?: string[];
  year?: number;
  summary?: string;
  signals: EvidenceSignals;
}

export interface EvidenceSignals {
  title_matches: boolean;
  author_matches?: boolean;
  source_volumes: number;
  local_books: number;
}

export interface PrefetchLaunch {
  run_id?: number;
  series: number;
}

export interface ResearchResult {
  series_id: number;
  query: string;
  status: string;
  run_id?: number;
  added: string[];
  dropped?: number;
  packet?: EvidencePacket;
}

export interface PendingPage {
  items: PendingSeries[];
  total: number;
  next_offset?: number;
  categories: PendingCategory[];
}

export interface PendingCategory {
  category: string;
  class: string;
  series: number;
}

export interface PendingSeries {
  series_id: number;
  library_name: string;
  directory_name: string;
  title: string;
  book_count: number;
  cover_book_id: number;
  identification: SeriesIdentification;
}

export interface OwnerIdentified {
  identification: SeriesIdentification;
  operation_id?: string;
}

