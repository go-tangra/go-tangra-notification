import { $t } from 'shell/locales';
import type { ChannelType, DeliveryStatus } from './api/client';

export function channelTypeList() {
  return [
    { value: 'CHANNEL_TYPE_EMAIL', label: $t('notification.page.channelType.EMAIL') },
    { value: 'CHANNEL_TYPE_SMS', label: $t('notification.page.channelType.SMS') },
    { value: 'CHANNEL_TYPE_SLACK', label: $t('notification.page.channelType.SLACK') },
    { value: 'CHANNEL_TYPE_SSE', label: $t('notification.page.channelType.SSE') },
  ];
}

export function channelTypeLabel(value: ChannelType): string {
  const item = channelTypeList().find((i) => i.value === value);
  return item ? item.label : '';
}

const CHANNEL_TYPE_COLOR_MAP: Record<string, string> = {
  CHANNEL_TYPE_EMAIL: '#165DFF',
  CHANNEL_TYPE_SMS: '#00B42A',
  CHANNEL_TYPE_SLACK: '#722ED1',
  CHANNEL_TYPE_SSE: '#F77234',
  DEFAULT: '#C9CDD4',
};

export function channelTypeColor(type: ChannelType): string {
  return CHANNEL_TYPE_COLOR_MAP[type] || CHANNEL_TYPE_COLOR_MAP.DEFAULT!;
}

export function deliveryStatusList() {
  return [
    { value: 'DELIVERY_STATUS_PENDING', label: $t('notification.page.deliveryStatus.PENDING') },
    { value: 'DELIVERY_STATUS_SENT', label: $t('notification.page.deliveryStatus.SENT') },
    { value: 'DELIVERY_STATUS_FAILED', label: $t('notification.page.deliveryStatus.FAILED') },
  ];
}

export function deliveryStatusLabel(value: DeliveryStatus): string {
  const item = deliveryStatusList().find((i) => i.value === value);
  return item ? item.label : '';
}

const DELIVERY_STATUS_COLOR_MAP: Record<string, string> = {
  DELIVERY_STATUS_PENDING: '#F77234',
  DELIVERY_STATUS_SENT: '#00B42A',
  DELIVERY_STATUS_FAILED: '#F53F3F',
  DEFAULT: '#C9CDD4',
};

export function deliveryStatusColor(status: DeliveryStatus): string {
  return DELIVERY_STATUS_COLOR_MAP[status] || DELIVERY_STATUS_COLOR_MAP.DEFAULT!;
}

export function enableBoolList() {
  return [
    { value: true, label: $t('notification.page.common.enabled') },
    { value: false, label: $t('notification.page.common.disabled') },
  ];
}

export function enableBoolToColor(value: boolean): string {
  return value ? '#00B42A' : '#C9CDD4';
}

export function enableBoolToName(value: boolean): string {
  return value ? $t('notification.page.common.enabled') : $t('notification.page.common.disabled');
}

// ---------- Internal Message ----------

import type { InternalMessage_Status, InternalMessage_Type, InternalMessageRecipient_Status } from './api/client';

export function internalMessageStatusList() {
  return [
    { value: 'DRAFT', label: $t('notification.page.internalMessage.statusDraft') },
    { value: 'PUBLISHED', label: $t('notification.page.internalMessage.statusPublished') },
    { value: 'SCHEDULED', label: $t('notification.page.internalMessage.statusScheduled') },
    { value: 'REVOKED', label: $t('notification.page.internalMessage.statusRevoked') },
    { value: 'ARCHIVED', label: $t('notification.page.internalMessage.statusArchived') },
    { value: 'DELETED', label: $t('notification.page.internalMessage.statusDeleted') },
  ];
}

export function internalMessageStatusLabel(value: InternalMessage_Status): string {
  const item = internalMessageStatusList().find((i) => i.value === value);
  return item ? item.label : '';
}

const INTERNAL_MESSAGE_STATUS_COLOR_MAP: Record<string, string> = {
  DRAFT: '#9CA3AF',
  PUBLISHED: '#00B42A',
  SCHEDULED: '#165DFF',
  REVOKED: '#F53F3F',
  ARCHIVED: '#86909C',
  DELETED: '#C9CDD4',
  DEFAULT: '#E5E7EB',
};

export function internalMessageStatusColor(status: InternalMessage_Status): string {
  return INTERNAL_MESSAGE_STATUS_COLOR_MAP[status] || INTERNAL_MESSAGE_STATUS_COLOR_MAP.DEFAULT!;
}

export function internalMessageTypeList() {
  return [
    { value: 'NOTIFICATION', label: $t('notification.page.internalMessage.typeNotification') },
    { value: 'PRIVATE', label: $t('notification.page.internalMessage.typePrivate') },
    { value: 'GROUP', label: $t('notification.page.internalMessage.typeGroup') },
  ];
}

export function internalMessageTypeLabel(value: InternalMessage_Type): string {
  const item = internalMessageTypeList().find((i) => i.value === value);
  return item ? item.label : '';
}

const INTERNAL_MESSAGE_TYPE_COLOR_MAP: Record<string, string> = {
  NOTIFICATION: '#165DFF',
  PRIVATE: '#722ED1',
  GROUP: '#00B42A',
  DEFAULT: '#C9CDD4',
};

export function internalMessageTypeColor(type: InternalMessage_Type): string {
  return INTERNAL_MESSAGE_TYPE_COLOR_MAP[type] || INTERNAL_MESSAGE_TYPE_COLOR_MAP.DEFAULT!;
}

export function internalMessageRecipientStatusList() {
  return [
    { value: 'SENT', label: $t('notification.page.internalMessage.recipientSent') },
    { value: 'RECEIVED', label: $t('notification.page.internalMessage.recipientReceived') },
    { value: 'READ', label: $t('notification.page.internalMessage.recipientRead') },
    { value: 'REVOKED', label: $t('notification.page.internalMessage.recipientRevoked') },
    { value: 'DELETED', label: $t('notification.page.internalMessage.recipientDeleted') },
  ];
}

export function internalMessageRecipientStatusLabel(value: InternalMessageRecipient_Status): string {
  const item = internalMessageRecipientStatusList().find((i) => i.value === value);
  return item ? item.label : '';
}

const INTERNAL_MESSAGE_RECIPIENT_COLOR_MAP: Record<string, string> = {
  SENT: '#4096FF',
  RECEIVED: '#165DFF',
  READ: '#86909C',
  REVOKED: '#F53F3F',
  DELETED: '#C9CDD4',
  DEFAULT: '#E5E7EB',
};

export function internalMessageRecipientStatusColor(status: InternalMessageRecipient_Status): string {
  return INTERNAL_MESSAGE_RECIPIENT_COLOR_MAP[status] || INTERNAL_MESSAGE_RECIPIENT_COLOR_MAP.DEFAULT!;
}
