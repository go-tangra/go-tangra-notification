export type Paging = { page?: number; pageSize?: number } | undefined;

export interface AdminUser {
  id?: number;
  username?: string;
  nickname?: string;
  realname?: string;
  avatar?: string;
  email?: string;
}

export interface AdminRole {
  id?: number;
  name?: string;
  code?: string;
  description?: string;
}
