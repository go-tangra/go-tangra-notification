// Domain types mirroring the notification OpenAPI contract (api/openapi/notification.yaml).
export type ChannelType = 'email' | 'sms' | 'slack' | 'sse'
export type Relation = 'owner' | 'editor' | 'viewer' | 'sharer'
export type ResourceType = 'channel' | 'template'
export type SubjectType = 'user' | 'role' | 'tenant'

export interface Permissions {
  read?: boolean
  write?: boolean
  delete?: boolean
  share?: boolean
  use?: boolean
}

export interface Page<T> {
  items: T[]
  next_cursor?: string
}

export type Settings = Record<string, unknown>

export interface Channel {
  id: string
  name: string
  type: ChannelType
  settings: Settings
  enabled: boolean
  is_default: boolean
  /** Created from the platform_email configuration: read-only, test sends allowed. */
  managed?: boolean
  template_count?: number
  created_by?: string
  updated_by?: string
  created_at?: string
  updated_at?: string
  permissions?: Permissions
}

export interface ChannelInput {
  name: string
  type: ChannelType
  settings: Settings
  enabled?: boolean
  is_default?: boolean
}

export interface Template {
  id: string
  name: string
  channel_id: string | null
  channel_name?: string
  channel_type?: ChannelType
  subject: string
  body: string
  variables?: string[]
  is_default?: boolean
  created_by?: string
  updated_by?: string
  created_at?: string
  updated_at?: string
  permissions?: Permissions
}

export interface TemplateInput {
  name: string
  channel_id: string
  subject: string
  body: string
  variables?: string[]
  is_default?: boolean
}

export interface LogEntry {
  id: string
  channel_id: string
  channel_type: ChannelType
  template_id: string | null
  recipient: string
  rendered_subject: string
  rendered_body?: string
  status: 'pending' | 'sent' | 'failed'
  error?: string
  sender_kind: 'user' | 'service'
  sender_id: string
  test: boolean
  created_at: string
  sent_at?: string | null
}

export interface Grant {
  id: string
  resource_type: ResourceType
  resource_id: string
  subject_type: SubjectType
  subject_id?: string
  relation: Relation
  granted_by?: string
  granted_at: string
  expires_at?: string | null
  expired?: boolean
}

export interface Source {
  id?: string
  resource_type: ResourceType
  resource_id: string
  subject_type: SubjectType
  subject_id?: string
  relation: Relation
  expires_at?: string | null
}

export interface Effective {
  relation: string
  permissions: Permissions
  grants: Source[]
}

export interface GrantInput {
  resource_type: ResourceType
  resource_id: string
  subject_type: SubjectType
  subject_id?: string | undefined
  relation: Relation
  expires_at?: string | null | undefined
}

export interface Category {
  id: string
  name: string
  description?: string
  sort: number
  message_count?: number
  created_by?: string
  updated_by?: string
  created_at?: string
  updated_at?: string
}

export interface CategoryInput {
  name: string
  description?: string
  sort?: number
}

export interface Recipients {
  all?: boolean
  users?: string[]
}

export type MessageStatus = 'draft' | 'scheduled' | 'publishing' | 'published' | 'revoked' | 'archived'

export interface Message {
  id: string
  title: string
  content: string
  type: 'notification' | 'private' | 'group'
  status: MessageStatus
  category_id?: string | null
  category_name?: string
  sender_id?: string
  recipients: Recipients
  recipient_count?: number
  read_count?: number
  scheduled_at?: string | null
  published_at?: string | null
  created_by?: string
  updated_by?: string
  created_at?: string
  updated_at?: string
}

export interface MessageInput {
  title: string
  content: string
  type?: 'notification' | 'private' | 'group'
  category_id?: string | null
  recipients: Recipients
  scheduled_at?: string | null
}

export interface SendResult {
  status: MessageStatus
  recipient_count: number
  dropped_recipients: string[]
}

export interface InboxMessage {
  id: string
  title: string
  content: string
  type: string
  category_name?: string
  sender_id?: string
  published_at?: string
}

export interface InboxEntry {
  id: string
  message: InboxMessage
  status: 'sent' | 'received' | 'read' | 'revoked'
  read_at?: string | null
  created_at: string
}

export interface InboxPage {
  items: InboxEntry[]
  unread: number
  next_cursor?: string
}

export interface EntityReport {
  created: number
  skipped: number
  overwritten: number
  failed: number
}

export interface BackupReport {
  channels: EntityReport
  templates: EntityReport
  categories: EntityReport
  warnings: string[]
}

export interface Stats {
  channels: number
  templates: number
  notifications: Record<string, number>
  messages: Record<string, number>
  open_streams: number
  operations_24h: number
}

export interface AuditItem {
  ts: string
  event_type: string
  actor_kind: string
  actor_id?: string
  subject_kind?: string
  subject_id?: string
  subject_name?: string
  outcome: string
  reason?: string
  details: Record<string, unknown>
}

export interface AuditFilter {
  event_type?: string | undefined
  actor_id?: string | undefined
  from?: string | undefined
  to?: string | undefined
}

export interface UserHit {
  id: string
  display_name: string
  avatar_url?: string
  email?: string
}

export interface RoleHit {
  slug: string
  display_name: string
}

/** Relations a granter holding `held` may hand out (never above their own). */
export function grantable(held: string): Relation[] {
  const order: Relation[] = ['viewer', 'sharer', 'editor', 'owner']
  const rank = order.indexOf(held as Relation)
  return rank < 0 ? [] : order.slice(0, rank + 1)
}

/** The credential fields hidden behind the "__set__" marker per channel type. */
export const SECRET_FIELDS: Record<ChannelType, string[]> = {
  email: ['password'],
  sms: ['api_key', 'token', 'password'],
  slack: ['api_key', 'token', 'password'],
  sse: ['api_key', 'token', 'password'],
}

export const SET_MARKER = '__set__'
