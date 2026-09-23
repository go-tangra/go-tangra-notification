import type { RouteRecordRaw } from 'vue-router'
import '@/main.css'

// Routes mounted by the platform shell under their own error boundary.
export const routes: RouteRecordRaw[] = [
  { path: '/notification/inbox', name: 'notification-inbox', component: () => import('@/views/inbox/index.vue'), meta: { module: 'notification' } },
  { path: '/notification/channels', name: 'notification-channels', component: () => import('@/views/channels/index.vue'), meta: { module: 'notification' } },
  { path: '/notification/templates', name: 'notification-templates', component: () => import('@/views/templates/index.vue'), meta: { module: 'notification' } },
  { path: '/notification/log', name: 'notification-log', component: () => import('@/views/log/index.vue'), meta: { module: 'notification' } },
  { path: '/notification/messages', name: 'notification-messages', component: () => import('@/views/messages/index.vue'), meta: { module: 'notification' } },
  { path: '/notification/categories', name: 'notification-categories', component: () => import('@/views/categories/index.vue'), meta: { module: 'notification' } },
  { path: '/notification/permissions', name: 'notification-permissions', component: () => import('@/views/permissions/index.vue'), meta: { module: 'notification' } },
]
export default routes
