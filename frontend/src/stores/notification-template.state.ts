import { defineStore } from 'pinia';

import { templateService } from '../api/client';
import type { CreateTemplateRequest, UpdateTemplateRequest } from '../api/client';
import type { Paging } from '../types';

export const useNotificationTemplateStore = defineStore(
  'notification-template',
  () => {
    async function listTemplates(paging?: Paging, formValues?: { name?: string; channelId?: string } | null) {
      return await templateService.ListTemplates({
        page: paging?.page,
        pageSize: paging?.pageSize,
        ...(formValues || {}),
      } as any);
    }

    async function getTemplate(id: string) {
      return await templateService.GetTemplate({ id });
    }

    async function createTemplate(data: CreateTemplateRequest) {
      return await templateService.CreateTemplate(data);
    }

    async function updateTemplate(id: string, data: UpdateTemplateRequest) {
      return await templateService.UpdateTemplate({ id, ...data });
    }

    async function deleteTemplate(id: string) {
      return await templateService.DeleteTemplate({ id });
    }

    async function previewTemplate(data: {
      subject: string;
      body: string;
      channelId?: string;
      variables?: Record<string, string>;
    }) {
      return await templateService.PreviewTemplate(data as any);
    }

    function $reset() {}

    return {
      $reset,
      listTemplates,
      getTemplate,
      createTemplate,
      updateTemplate,
      deleteTemplate,
      previewTemplate,
    };
  },
);
