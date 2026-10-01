<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { UiPage, UiCard, UiDataTable, UiPermissionDrawer, usePermissionGrants, useListQuery, type Column } from '@go-tangra/ui'
import { CHANNEL_LIST, useChannels } from '@/stores/channels'
import { TEMPLATE_LIST, useTemplates } from '@/stores/templates'
import { usePermissions } from '@/stores/permissions'
import { useDirectory } from '@/stores/directory'
import { grantable, type Channel, type Relation, type ResourceType, type SubjectType, type Template } from '@/api/types'

const channels = useChannels()
const templates = useTemplates()
const store = usePermissions()
const dir = useDirectory()
const drawer = ref(false)
const target = ref<{ type: ResourceType; id: string; name: string } | null>(null)
const perms = usePermissionGrants({
  grants: () => store.grants,
  effective: () => ({ relation: store.effective?.relation, canShare: store.effective?.permissions?.share ?? false }),
  grant: (r) => store.grant({ resource_type: target.value!.type, resource_id: target.value!.id, subject_type: r.subject_type as SubjectType, subject_id: r.subject_id, relation: r.relation as Relation, expires_at: r.expires_at }),
  revoke: (id) => store.revoke(id),
  directory: { roles: () => Object.values(dir.roles), searchUsers: dir.searchUsers, resolveUsers: dir.resolveUsers, userName: dir.userName, roleName: dir.roleName },
  grantable,
})
// --- two server-paged tables (?perm-channels.page=…, ?perm-templates.page=…) ---
const cq = useListQuery('perm-channels', CHANNEL_LIST.opts)
const tq = useListQuery('perm-templates', TEMPLATE_LIST.opts)
async function loadChannels(): Promise<void> {
  const res = await channels.list({}, cq.query.value)
  if (res?.page) cq.clampTo(res.page)
}
async function loadTemplates(): Promise<void> {
  const res = await templates.list({}, tq.query.value)
  if (res?.page) tq.clampTo(res.page)
}
watch(cq.query, () => void loadChannels())
watch(tq.query, () => void loadTemplates())
onMounted(async () => {
  await Promise.all([loadChannels(), loadTemplates(), dir.loadRoles()])
})
async function open(type: ResourceType, id: string, name: string): Promise<void> {
  target.value = { type, id, name }
  drawer.value = true
  await store.load(type, id)
  await perms.resolve()
}
const channelColumns: Column<Channel>[] = [{ key: 'name', label: 'Name', sortable: true }, { key: 'type', label: 'Type', width: 'sm', sortable: true }]
const templateColumns: Column<Template>[] = [{ key: 'name', label: 'Name', sortable: true }, { key: 'channel', label: 'Channel', format: (t) => t.channel_name ?? '', sortable: true, hideOnStack: true }]
</script>

<template>
  <UiPage title="Permissions" subtitle="Who can read, edit, share or own each channel and template">
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <UiCard title="Channels" :padded="false" data-test="perm-channels">
        <UiDataTable :items="channels.items" :columns="channelColumns" :loading="channels.loading" :total="channels.total" :page="cq.page.value" :page-size="cq.pageSize.value" :sort="cq.sort.value" caption="Channels" empty-title="No channels" clickable :row-attrs="(c) => ({ 'data-test': 'perm-channel-' + c.id })" @row-click="open('channel', $event.id, $event.name)" @update:page="cq.setPage" @update:page-size="cq.setPageSize" @update:sort="cq.setSort" />
      </UiCard>
      <UiCard title="Templates" :padded="false" data-test="perm-templates">
        <UiDataTable :items="templates.items" :columns="templateColumns" :loading="templates.loading" :total="templates.total" :page="tq.page.value" :page-size="tq.pageSize.value" :sort="tq.sort.value" caption="Templates" empty-title="No templates" clickable :row-attrs="(t) => ({ 'data-test': 'perm-template-' + t.id })" @row-click="open('template', $event.id, $event.name)" @update:page="tq.setPage" @update:page-size="tq.setPageSize" @update:sort="tq.setSort" />
      </UiCard>
    </div>
    <UiPermissionDrawer v-model="drawer" :title="'Permissions — ' + (target?.name || target?.id || '')" :grants="perms.grants.value" :subjects="perms.subjects.value" :levels="perms.levels.value" :can-manage="perms.canShare.value" expires :editable-level="false" :hint="perms.hint.value" :error="perms.error.value" :busy="perms.busy.value" data-test="permission-drawer" @search="perms.search" @grant="perms.onGrant" @revoke="perms.onRevoke" @change-level="perms.onChangeLevel" />
  </UiPage>
</template>
