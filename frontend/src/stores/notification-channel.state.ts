import { defineStore } from 'pinia';

import {
  ChannelService,
  type CreateChannelRequest,
  type UpdateChannelRequest,
} from '../api/services';
import type { Paging } from '../types';

export const useNotificationChannelStore = defineStore(
  'notification-channel',
  () => {
    async function listChannels(paging?: Paging, formValues?: { name?: string; type?: string } | null) {
      return await ChannelService.list({
        page: paging?.page,
        pageSize: paging?.pageSize,
        ...(formValues || {}),
      } as any);
    }

    async function getChannel(id: string) {
      return await ChannelService.get(id);
    }

    async function createChannel(data: CreateChannelRequest) {
      return await ChannelService.create(data);
    }

    async function updateChannel(id: string, data: UpdateChannelRequest) {
      return await ChannelService.update(id, data);
    }

    async function deleteChannel(id: string) {
      return await ChannelService.delete(id);
    }

    function $reset() {}

    return {
      $reset,
      listChannels,
      getChannel,
      createChannel,
      updateChannel,
      deleteChannel,
    };
  },
);
