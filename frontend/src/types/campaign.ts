// Backend sözleşmesinin (internal/campaign, internal/audience) TS karşılığı.

export type CampaignStatus =
  | 'draft'
  | 'analyzed'
  | 'generated'
  | 'approved'
  | 'sending'
  | 'completed'
  | 'completed_with_errors'

export type Rule =
  | { field: 'city'; op: 'eq'; value: string }
  | { field: 'city'; op: 'in'; value: string[] }
  | { field: 'cart_added_within_days'; op: 'lte'; value: number }
  | { field: 'purchased_since_last_cart'; op: 'eq'; value: boolean }
  | { field: 'purchased_within_days'; op: 'lte' | 'gt'; value: number }
  | { field: 'total_orders'; op: 'gte' | 'lte' | 'eq'; value: number }

export interface SegmentPlan {
  summary: string
  rules: Rule[]
  unsupported_criteria: string[]
}

export interface Content {
  sms_text: string
  email_subject: string
  email_body: string
}

export type ContentField = keyof Content

export interface FieldCheck {
  template_length: number
  rendered_max: number
  limit: number
  ok: boolean
}

export interface Checks {
  sms_text: FieldCheck
  email_subject: FieldCheck
  email_body: FieldCheck
  preview_vars: { first_name: string; city: string }
}

export interface Campaign {
  id: number
  brief: string
  status: CampaignStatus
  version: number
  segment: SegmentPlan | null
  content: Content | null
  created_at: string
  updated_at: string
}

export interface Reach {
  matched: number
  sms_eligible: number
  email_eligible: number
  reachable: number
}

export interface QueuedCounts {
  sms: number
  email: number
}

export interface CampaignView {
  campaign: Campaign
  reach: Reach | null
  checks: Checks | null
  queued: QueuedCounts | null
}

export interface AnalyzeResult extends CampaignView {
  repaired: boolean
}

export interface Issue {
  path: string
  code: string
  message: string
}

export interface ChannelStats {
  total: number
  queued: number
  in_flight: number
  retry_wait: number
  sent: number
  failed: number
  skipped: number
  sent_per_sec: number
  requests_per_sec: number
}

export interface AttemptEvent {
  delivery_id: number
  channel: 'sms' | 'email'
  attempt_no: number
  http_status: number | null
  outcome: 'sent' | 'retry' | 'failed' | 'timeout' | 'skipped'
  duration_ms: number
  at: string
}

export interface MonitorStats {
  campaign_id: number
  status: CampaignStatus
  channels: Partial<Record<'sms' | 'email', ChannelStats>>
  error_codes: Record<string, number>
  recent: AttemptEvent[]
  approved_at: string | null
  completed_at: string | null
  at: string
}
