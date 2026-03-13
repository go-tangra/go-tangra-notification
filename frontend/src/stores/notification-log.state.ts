import { defineStore } from 'pinia';

import { notificationService } from '../api/client';
import type { SendNotificationRequest } from '../api/client';
import type { Paging } from '../types';

export const useNotificationLogStore = defineStore(
  'notification-log',
  () => {
    async function listNotifications(
      paging?: Paging,
      formValues?: { status?: string; channelType?: string } | null,
    ) {
      return await notificationService.ListNotifications({
        page: paging?.page,
        pageSize: paging?.pageSize,
        ...(formValues || {}),
      } as any);
    }

    async function getNotification(id: string) {
      return await notificationService.GetNotification({ id });
    }

    async function sendNotification(data: SendNotificationRequest) {
      return await notificationService.SendNotification(data);
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
