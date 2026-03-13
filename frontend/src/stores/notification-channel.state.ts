import { defineStore } from 'pinia';

import { channelService } from '../api/client';
import type { CreateChannelRequest, UpdateChannelRequest } from '../api/client';
import type { Paging } from '../types';

export const useNotificationChannelStore = defineStore(
  'notification-channel',
  () => {
    async function listChannels(paging?: Paging, formValues?: { name?: string; type?: string } | null) {
      return await channelService.ListChannels({
        page: paging?.page,
        pageSize: paging?.pageSize,
        ...(formValues || {}),
      } as any);
    }

    async function getChannel(id: string) {
      return await channelService.GetChannel({ id });
    }

    async function createChannel(data: CreateChannelRequest) {
      return await channelService.CreateChannel(data);
    }

    async function updateChannel(id: string, data: UpdateChannelRequest) {
      return await channelService.UpdateChannel({ id, ...data });
    }

    async function deleteChannel(id: string) {
      return await channelService.DeleteChannel({ id });
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
