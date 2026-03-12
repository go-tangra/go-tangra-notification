import { defineStore } from 'pinia';

import {
  NotificationLogService,
  type SendNotificationRequest,
} from '../api/services';
import type { Paging } from '../types';

export const useNotificationLogStore = defineStore(
  'notification-log',
  () => {
    async function listNotifications(
      paging?: Paging,
      formValues?: { status?: string; channelType?: string } | null,
    ) {
      return await NotificationLogService.list({
        page: paging?.page,
        pageSize: paging?.pageSize,
        ...(formValues || {}),
      });
    }

    async function getNotification(id: string) {
      return await NotificationLogService.get(id);
    }

    async function sendNotification(data: SendNotificationRequest) {
      return await NotificationLogService.send(data);
    }

    function $reset() {}

    return {
      $reset,
      listNotifications,
      getNotification,
      sendNotification,
    };
  },
);
