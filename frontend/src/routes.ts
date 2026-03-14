import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    path: '/notification',
    name: 'Notification',
    component: () => import('shell/app-layout'),
    redirect: '/notification/channels',
    meta: {
      order: 2010,
      icon: 'lucide:bell',
      title: 'notification.menu.notification',
      keepAlive: true,
      authority: ['platform:admin', 'tenant:manager'],
    },
    children: [
      {
        path: 'channels',
        name: 'NotificationChannels',
        meta: {
          icon: 'lucide:radio',
          title: 'notification.menu.channel',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/channel/index.vue'),
      },
      {
        path: 'templates',
        name: 'NotificationTemplates',
        meta: {
          icon: 'lucide:file-text',
          title: 'notification.menu.template',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/template/index.vue'),
      },
      {
        path: 'logs',
        name: 'NotificationLogs',
        meta: {
          icon: 'lucide:scroll-text',
          title: 'notification.menu.log',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/log/index.vue'),
      },
      {
        path: 'permissions',
        name: 'NotificationPermissions',
        meta: {
          icon: 'lucide:shield',
          title: 'notification.menu.permissions',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/permission/index.vue'),
      },
      {
        path: 'messages',
        name: 'InternalMessageList',
        meta: {
          icon: 'lucide:message-circle-more',
          title: 'notification.menu.internalMessage',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/internal-message/message/index.vue'),
      },
      {
        path: 'categories',
        name: 'InternalMessageCategoryManagement',
        meta: {
          icon: 'lucide:calendar-check',
          title: 'notification.menu.internalMessageCategory',
          authority: ['platform:admin'],
        },
        component: () => import('./views/internal-message/category/index.vue'),
      },
    ],
  },
];

export default routes;
