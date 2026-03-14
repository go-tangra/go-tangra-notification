import './styles/tailwind.css';
import type { TangraModule } from './sdk';
import routes from './routes';
import { useNotificationChannelStore } from './stores/notification-channel.state';
import { useNotificationTemplateStore } from './stores/notification-template.state';
import { useNotificationLogStore } from './stores/notification-log.state';
import { useNotificationPermissionStore } from './stores/notification-permission.state';
import { useInternalMessageStore } from './stores/internal-message.state';
import { useInternalMessageCategoryStore } from './stores/internal-message-category.state';
import enUS from './locales/en-US.json';

const notificationModule: TangraModule = {
  id: 'notification',
  version: '1.0.0',
  routes,
  stores: {
    'notification-channel': useNotificationChannelStore,
    'notification-template': useNotificationTemplateStore,
    'notification-log': useNotificationLogStore,
    'notification-permission': useNotificationPermissionStore,
    'notification-internal-message': useInternalMessageStore,
    'notification-internal-message-category': useInternalMessageCategoryStore,
  },
  locales: {
    'en-US': enUS,
  },
};

export default notificationModule;
