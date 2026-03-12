import { notificationApi } from './client';

export async function listUsers(): Promise<{ items: any[] }> {
  return notificationApi.get('/users?noPaging=true');
}

export async function listRoles(): Promise<{ items: any[] }> {
  return notificationApi.get('/roles?noPaging=true');
}
