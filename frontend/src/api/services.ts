import { notificationApi, type RequestOptions } from './client';

// ==================== Entity Types ====================

export type ChannelType =
  | 'CHANNEL_TYPE_EMAIL'
  | 'CHANNEL_TYPE_SLACK'
  | 'CHANNEL_TYPE_SMS'
  | 'CHANNEL_TYPE_SSE';

export type DeliveryStatus =
  | 'DELIVERY_STATUS_FAILED'
  | 'DELIVERY_STATUS_PENDING'
  | 'DELIVERY_STATUS_SENT';

export type ResourceType =
  | 'RESOURCE_TYPE_TEMPLATE'
  | 'RESOURCE_TYPE_CHANNEL';

export type RelationType =
  | 'RELATION_OWNER'
  | 'RELATION_EDITOR'
  | 'RELATION_VIEWER'
  | 'RELATION_SHARER';

export type SubjectType =
  | 'SUBJECT_TYPE_USER'
  | 'SUBJECT_TYPE_ROLE'
  | 'SUBJECT_TYPE_CLIENT';

export type PermissionAction =
  | 'PERMISSION_ACTION_READ'
  | 'PERMISSION_ACTION_WRITE'
  | 'PERMISSION_ACTION_DELETE'
  | 'PERMISSION_ACTION_SHARE'
  | 'PERMISSION_ACTION_USE';

export interface NotificationChannel {
  id: string;
  tenantId: number;
  name: string;
  type: ChannelType;
  config: string;
  enabled: boolean;
  isDefault: boolean;
  createdBy?: number;
  updatedBy?: number;
  createTime: string;
  updateTime?: string;
}

export interface NotificationTemplate {
  id: string;
  tenantId: number;
  name: string;
  channelId: string;
  channelType?: ChannelType;
  subject: string;
  body: string;
  variables: string;
  isDefault: boolean;
  createdBy?: number;
  updatedBy?: number;
  createTime: string;
  updateTime?: string;
}

export interface NotificationLog {
  id: string;
  tenantId: number;
  channelId: string;
  channelType: ChannelType;
  templateId: string;
  recipient: string;
  renderedSubject: string;
  renderedBody: string;
  status: DeliveryStatus;
  errorMessage: string;
  sentAt?: string;
  createTime: string;
}

export interface NotificationPermission {
  id: number;
  tenantId: number;
  resourceType: ResourceType;
  resourceId: string;
  relation: RelationType;
  subjectType: SubjectType;
  subjectId: string;
  grantedBy?: number;
  expiresAt?: string;
  createTime?: string;
}

// ==================== Request/Response Types ====================

export interface CreateChannelRequest {
  name: string;
  type: ChannelType;
  config: string;
  enabled?: boolean;
  isDefault?: boolean;
}

export interface UpdateChannelRequest {
  name?: string;
  config?: string;
  enabled?: boolean;
  isDefault?: boolean;
}

export interface ListChannelsResponse {
  channels: NotificationChannel[];
  total: number;
}

export interface CreateTemplateRequest {
  name: string;
  channelId: string;
  subject: string;
  body: string;
  variables?: string;
  isDefault?: boolean;
}

export interface UpdateTemplateRequest {
  name?: string;
  channelId?: string;
  subject?: string;
  body?: string;
  variables?: string;
  isDefault?: boolean;
}

export interface ListTemplatesResponse {
  templates: NotificationTemplate[];
  total: number;
}

export interface PreviewTemplateResponse {
  renderedSubject: string;
  renderedBody: string;
}

export interface SendNotificationRequest {
  templateId: string;
  recipient: string;
  variables: Record<string, string>;
  channelId?: string;
}

export interface SendNotificationResponse {
  notification: NotificationLog;
}

export interface ListNotificationsResponse {
  notifications: NotificationLog[];
  total: number;
}

export interface GrantAccessRequest {
  resourceType: ResourceType;
  resourceId: string;
  relation: RelationType;
  subjectType: SubjectType;
  subjectId: string;
  expiresAt?: string;
}

export interface GrantAccessResponse {
  permission: NotificationPermission;
}

export interface ListPermissionsResponse {
  permissions: NotificationPermission[];
  total: number;
}

export interface CheckAccessRequest {
  resourceType: ResourceType;
  resourceId: string;
  subjectType: SubjectType;
  subjectId: string;
  permission: PermissionAction;
}

export interface CheckAccessResponse {
  allowed: boolean;
  reason: string;
  relation?: RelationType;
}

export interface GetEffectivePermissionsResponse {
  permissions: PermissionAction[];
  highestRelation: RelationType;
}

// ==================== Helper Functions ====================

function buildQuery(params: Record<string, unknown>): string {
  const searchParams = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== '') {
      if (Array.isArray(value)) {
        value.forEach(v => searchParams.append(key, String(v)));
      } else {
        searchParams.append(key, String(value));
      }
    }
  }
  const query = searchParams.toString();
  return query ? `?${query}` : '';
}

// ==================== Channel Service ====================

export const ChannelService = {
  create: (data: CreateChannelRequest, options?: RequestOptions) =>
    notificationApi.post<{ channel: NotificationChannel }>('/channels', data, options),

  get: (id: string, options?: RequestOptions) =>
    notificationApi.get<{ channel: NotificationChannel }>(`/channels/${id}`, options),

  list: (params?: { page?: number; pageSize?: number; type?: ChannelType }, options?: RequestOptions) =>
    notificationApi.get<ListChannelsResponse>(`/channels${buildQuery(params || {})}`, options),

  update: (id: string, data: UpdateChannelRequest, options?: RequestOptions) =>
    notificationApi.put<{ channel: NotificationChannel }>(`/channels/${id}`, data, options),

  delete: (id: string, options?: RequestOptions) =>
    notificationApi.delete<void>(`/channels/${id}`, options),
};

// ==================== Template Service ====================

export const TemplateService = {
  create: (data: CreateTemplateRequest, options?: RequestOptions) =>
    notificationApi.post<{ template: NotificationTemplate }>('/templates', data, options),

  get: (id: string, options?: RequestOptions) =>
    notificationApi.get<{ template: NotificationTemplate }>(`/templates/${id}`, options),

  list: (params?: { page?: number; pageSize?: number; channelId?: string }, options?: RequestOptions) =>
    notificationApi.get<ListTemplatesResponse>(`/templates${buildQuery(params || {})}`, options),

  update: (id: string, data: UpdateTemplateRequest, options?: RequestOptions) =>
    notificationApi.put<{ template: NotificationTemplate }>(`/templates/${id}`, data, options),

  delete: (id: string, options?: RequestOptions) =>
    notificationApi.delete<void>(`/templates/${id}`, options),

  preview: (data: { subject: string; body: string; channelId?: string; variables?: Record<string, string> }, options?: RequestOptions) =>
    notificationApi.post<PreviewTemplateResponse>('/templates/preview', data, options),
};

// ==================== Notification Log Service ====================

export const NotificationLogService = {
  send: (data: SendNotificationRequest, options?: RequestOptions) =>
    notificationApi.post<SendNotificationResponse>('/notifications/send', data, options),

  get: (id: string, options?: RequestOptions) =>
    notificationApi.get<{ notification: NotificationLog }>(`/notifications/${id}`, options),

  list: (params?: { page?: number; pageSize?: number; status?: string; channelType?: string }, options?: RequestOptions) =>
    notificationApi.get<ListNotificationsResponse>(`/notifications${buildQuery(params || {})}`, options),
};

// ==================== Permission Service ====================

export const PermissionService = {
  grant: (data: GrantAccessRequest, options?: RequestOptions) =>
    notificationApi.post<GrantAccessResponse>('/permissions', data, options),

  revoke: (params?: {
    resourceType?: ResourceType;
    resourceId?: string;
    subjectType?: SubjectType;
    subjectId?: string;
    relation?: RelationType;
  }, options?: RequestOptions) =>
    notificationApi.delete<void>(`/permissions${buildQuery(params || {})}`, options),

  list: (params?: {
    resourceType?: ResourceType;
    resourceId?: string;
    subjectType?: SubjectType;
    subjectId?: string;
    page?: number;
    pageSize?: number;
  }, options?: RequestOptions) =>
    notificationApi.get<ListPermissionsResponse>(`/permissions${buildQuery(params || {})}`, options),

  check: (data: CheckAccessRequest, options?: RequestOptions) =>
    notificationApi.post<CheckAccessResponse>('/permissions/check', data, options),

  getEffective: (params?: {
    resourceType?: ResourceType;
    resourceId?: string;
    subjectType?: SubjectType;
    subjectId?: string;
  }, options?: RequestOptions) =>
    notificationApi.get<GetEffectivePermissionsResponse>(`/permissions/effective${buildQuery(params || {})}`, options),
};
