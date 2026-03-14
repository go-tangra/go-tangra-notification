import { defineStore } from 'pinia';

import { internalMessageCategoryService } from '../api/client';
import type { Paging } from '../types';

export const useInternalMessageCategoryStore = defineStore(
  'notification-internal-message-category',
  () => {
    async function listInternalMessageCategory(
      paging?: Paging,
      formValues?: Record<string, string | undefined> | null,
    ) {
      const filterObj: Record<string, unknown> = {};
      if (formValues) {
        for (const [key, val] of Object.entries(formValues)) {
          if (val !== undefined && val !== null && val !== '') {
            filterObj[key] = val;
          }
        }
      }
      return await internalMessageCategoryService.List({
        page: paging?.page,
        pageSize: paging?.pageSize,
        query: Object.keys(filterObj).length > 0 ? JSON.stringify(filterObj) : undefined,
      });
    }

    async function getInternalMessageCategory(id: number) {
      return await internalMessageCategoryService.Get({ id });
    }

    async function createInternalMessageCategory(values: Record<string, unknown>) {
      return await internalMessageCategoryService.Create({
        data: { ...values },
      });
    }

    async function updateInternalMessageCategory(id: number, values: Record<string, unknown>) {
      const paths = Object.keys(values);
      return await internalMessageCategoryService.Update({
        id,
        data: { ...values },
        updateMask: { paths },
      });
    }

    async function deleteInternalMessageCategory(id: number) {
      return await internalMessageCategoryService.Delete({ id });
    }

    function $reset() {}

    return {
      $reset,
      listInternalMessageCategory,
      getInternalMessageCategory,
      createInternalMessageCategory,
      updateInternalMessageCategory,
      deleteInternalMessageCategory,
    };
  },
);
