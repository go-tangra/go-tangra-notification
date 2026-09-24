<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { UiPage, UiCard, UiDataTable, UiPermissionDrawer, usePermissionGrants, type Column } from '@go-tangra/ui'
import { useChannels } from '@/stores/channels'
import { useTemplates } from '@/stores/templates'
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
onMounted(async () => {
  await Promise.all([channels.list(), templates.list(), dir.loadRoles()])
})
async function open(type: ResourceType, id: string, name: string): Promise<void> {
  target.value = { type, id, name }
  drawer.value = true
  await store.load(type, id)
  await perms.resolve()
}
const channelColumns: Column<Channel>[] = [{ key: 'name', label: 'Name' }, { key: 'type', label: 'Type', width: 'sm' }]
const templateColumns: Column<Template>[] = [{ key: 'name', label: 'Name' }, { key: 'channel_name', label: 'Channel', hideOnStack: true }]
</script>

<template>
  <UiPage title="Permissions" subtitle="Who can read, edit, share or own each channel and template">
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <UiCard title="Channels" :padded="false" data-test="perm-channels">
        <UiDataTable :items="channels.items" :columns="channelColumns" caption="Channels" empty-title="No channels" clickable :row-attrs="(c) => ({ 'data-test': 'perm-channel-' + c.id })" @row-click="open('channel', $event.id, $event.name)" />
      </UiCard>
      <UiCard title="Templates" :padded="false" data-test="perm-templates">
        <UiDataTable :items="templates.items" :columns="templateColumns" caption="Templates" empty-title="No templates" clickable :row-attrs="(t) => ({ 'data-test': 'perm-template-' + t.id })" @row-click="open('template', $event.id, $event.name)" />
      </UiCard>
    </div>
    <UiPermissionDrawer v-model="drawer" :title="'Permissions — ' + (target?.name || target?.id || '')" :grants="perms.grants.value" :subjects="perms.subjects.value" :levels="perms.levels.value" :can-manage="perms.canShare.value" expires :editable-level="false" :hint="perms.hint.value" :error="perms.error.value" :busy="perms.busy.value" data-test="permission-drawer" @search="perms.search" @grant="perms.onGrant" @revoke="perms.onRevoke" @change-level="perms.onChangeLevel" />
  </UiPage>
</template>
