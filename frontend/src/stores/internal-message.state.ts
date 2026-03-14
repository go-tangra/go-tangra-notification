import { defineStore } from 'pinia';

import { internalMessageService, internalMessageRecipientService } from '../api/client';
import type { SendMessageRequest } from '../api/client';
import type { Paging } from '../types';

export const useInternalMessageStore = defineStore(
  'notification-internal-message',
  () => {
    async function listMessage(
      paging?: Paging,
      formValues?: { status?: string; type?: string; category_id?: string } | null,
    ) {
      const queryParts: string[] = [];
      if (formValues) {
        for (const [key, val] of Object.entries(formValues)) {
          if (val !== undefined && val !== null && val !== '') {
            queryParts.push(`${key}=${val}`);
          }
        }
      }
      return await internalMessageService.ListMessage({
        page: paging?.page,
        pageSize: paging?.pageSize,
        query: queryParts.length > 0 ? queryParts.join('&') : undefined,
      });
    }

    async function getMessage(id: number) {
      return await internalMessageService.GetMessage({ id });
    }

    async function updateMessage(id: number, values: Record<string, unknown>) {
      const paths = Object.keys(values);
      return await internalMessageService.UpdateMessage({
        id,
        data: { ...values },
        updateMask: { paths },
      });
    }

    async function deleteMessage(id: number) {
      return await internalMessageService.DeleteMessage({ id });
    }

    async function sendMessage(request: SendMessageRequest) {
      return await internalMessageService.SendMessage(request);
    }

    async function revokeMessage(userId: number, messageId: number) {
      return await internalMessageService.RevokeMessage({ messageId, userId });
    }

    async function listUserInbox(
      paging?: Paging,
      formValues?: Record<string, string | undefined> | null,
      _fieldMask?: null | string,
      orderBy?: null | string[],
    ) {
      const queryParts: string[] = [];
      if (formValues) {
        for (const [key, val] of Object.entries(formValues)) {
          if (val !== undefined && val !== null && val !== '') {
            queryParts.push(`${key}=${val}`);
          }
        }
      }
      return await internalMessageRecipientService.ListUserInbox({
        page: paging?.page,
        pageSize: paging?.pageSize,
        query: queryParts.length > 0 ? queryParts.join('&') : undefined,
        orderBy: orderBy ? orderBy.join(',') : undefined,
      });
    }

    async function markNotificationAsRead(userId: number, recipientIds: number[]) {
      return await internalMessageRecipientService.MarkNotificationAsRead({
        userId,
        recipientIds,
      });
    }

    async function deleteNotificationFromInbox(userId: number, recipientIds: number[]) {
      return await internalMessageRecipientService.DeleteNotificationFromInbox({
        userId,
        recipientIds,
      });
    }

    function $reset() {}

    return {
      $reset,
      listMessage,
      getMessage,
      updateMessage,
      deleteMessage,
      sendMessage,
      revokeMessage,
      listUserInbox,
      markNotificationAsRead,
      deleteNotificationFromInbox,
    };
  },
);
