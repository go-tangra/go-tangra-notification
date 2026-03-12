import { defineStore } from 'pinia';

import {
  TemplateService,
  type CreateTemplateRequest,
  type UpdateTemplateRequest,
} from '../api/services';
import type { Paging } from '../types';

export const useNotificationTemplateStore = defineStore(
  'notification-template',
  () => {
    async function listTemplates(paging?: Paging, formValues?: { name?: string; channelId?: string } | null) {
      return await TemplateService.list({
        page: paging?.page,
        pageSize: paging?.pageSize,
        ...(formValues || {}),
      } as any);
    }

    async function getTemplate(id: string) {
      return await TemplateService.get(id);
    }

    async function createTemplate(data: CreateTemplateRequest) {
      return await TemplateService.create(data);
    }

    async function updateTemplate(id: string, data: UpdateTemplateRequest) {
      return await TemplateService.update(id, data);
    }

    async function deleteTemplate(id: string) {
      return await TemplateService.delete(id);
    }

    async function previewTemplate(data: {
      subject: string;
      body: string;
      variables?: Record<string, string>;
    }) {
      return await TemplateService.preview(data);
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
