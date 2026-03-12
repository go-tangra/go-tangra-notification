import { $t } from 'shell/locales';
import type { ChannelType, DeliveryStatus } from './api/services';

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
