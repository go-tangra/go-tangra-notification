import { useAccessStore } from 'shell/vben/stores';

import {
  createNotificationChannelServiceClient,
  createNotificationServiceClient,
  createNotificationTemplateServiceClient,
  createNotificationPermissionServiceClient,
  createNotificationUserServiceClient,
  createInternalMessageServiceClient,
  createInternalMessageRecipientServiceClient,
  createInternalMessageCategoryServiceClient,
} from '../generated/api/notification/service/v1';

const MODULE_BASE_URL = '/admin/v1/modules/notification';

type RequestType = { path: string; method: string; body: string | null };

async function handler(req: RequestType): Promise<unknown> {
  const accessStore = useAccessStore();
  const token = (accessStore as any).accessToken;

  const response = await fetch(`${MODULE_BASE_URL}/${req.path}`, {
    method: req.method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: req.body,
  });

  if (!response.ok) {
    let message = `HTTP error! status: ${response.status}`;
    try {
      const errorBody = await response.json();
      if (errorBody?.message) {
        message = errorBody.message;
      }
    } catch {}
    throw new Error(message);
  }

  const text = await response.text();
  return text ? JSON.parse(text) : {};
}

export const channelService = createNotificationChannelServiceClient(handler);
export const notificationService = createNotificationServiceClient(handler);
export const templateService = createNotificationTemplateServiceClient(handler);
export const permissionService = createNotificationPermissionServiceClient(handler);
export const userService = createNotificationUserServiceClient(handler);
export const internalMessageService = createInternalMessageServiceClient(handler);
export const internalMessageRecipientService = createInternalMessageRecipientServiceClient(handler);
export const internalMessageCategoryService = createInternalMessageCategoryServiceClient(handler);

// Re-export generated types for convenience
export type {
  NotificationChannel,
  ChannelType,
  CreateChannelRequest,
  UpdateChannelRequest,
  ListChannelsResponse,
  NotificationLog,
  DeliveryStatus,
  SendNotificationRequest,
  SendNotificationResponse,
  ListNotificationsResponse,
  NotificationTemplate,
  CreateTemplateRequest,
  UpdateTemplateRequest,
  ListTemplatesResponse,
  PreviewTemplateResponse,
  NotificationPermission,
  ResourceType,
  Relation,
  SubjectType,
  PermissionAction,
  GrantAccessRequest,
  GrantAccessResponse,
  CheckAccessRequest,
  CheckAccessResponse,
  GetEffectivePermissionsResponse,
  ListPermissionsResponse,
  NotificationUser,
  NotificationRole,
  ListNotificationUsersResponse,
  ListNotificationRolesResponse,
  InternalMessage,
  InternalMessage_Status,
  InternalMessage_Type,
  InternalMessageRecipient,
  InternalMessageRecipient_Status,
  InternalMessageCategory,
  SendMessageRequest,
  SendMessageResponse,
  ListInternalMessageResponse,
  ListUserInboxResponse,
  ListInternalMessageCategoryResponse,
} from '../generated/api/notification/service/v1';
