// Code generated from internal_message protos. DO NOT EDIT.
/* eslint-disable camelcase */
// @ts-nocheck

// Encoded using RFC 3339, where generated output will always be Z-normalized
// and uses 0, 3, 6 or 9 fractional digits.
// Offsets other than "Z" are also accepted.
type wellKnownTimestamp = string;

// An empty JSON object
type wellKnownEmpty = Record<never, never>;

type RequestType = {
  path: string;
  method: string;
  body: string | null;
};

type RequestHandler = (request: RequestType, meta: { service: string, method: string }) => Promise<unknown>;

// ---------- Internal Message ----------

export type InternalMessage_Status =
  | "DRAFT"
  | "PUBLISHED"
  | "SCHEDULED"
  | "REVOKED"
  | "ARCHIVED"
  | "DELETED";

export type InternalMessage_Type =
  | "NOTIFICATION"
  | "PRIVATE"
  | "GROUP";

export type InternalMessage = {
  id?: number;
  title?: string;
  content?: string;
  status?: InternalMessage_Status;
  type?: InternalMessage_Type;
  senderId?: number;
  senderName?: string;
  categoryId?: number;
  categoryName?: string;
  tenantId?: number;
  tenantName?: string;
  createdBy?: number;
  updatedBy?: number;
  deletedBy?: number;
  createdAt?: wellKnownTimestamp;
  updatedAt?: wellKnownTimestamp;
  deletedAt?: wellKnownTimestamp;
};

export type ListInternalMessageResponse = {
  items: InternalMessage[] | undefined;
  total: number | undefined;
};

export type GetInternalMessageRequest = {
  id: number;
};

export type CreateInternalMessageRequest = {
  data: Partial<InternalMessage>;
};

export type UpdateInternalMessageRequest = {
  id: number;
  data: Partial<InternalMessage>;
  updateMask?: { paths: string[] };
  allowMissing?: boolean;
};

export type DeleteInternalMessageRequest = {
  id: number;
};

export type SendMessageRequest = {
  type?: InternalMessage_Type;
  recipientUserId?: number;
  conversationId?: number;
  categoryId?: number;
  targetUserIds?: number[];
  targetAll?: boolean;
  title?: string;
  content: string;
};

export type SendMessageResponse = {
  messageId: number | undefined;
};

export type RevokeMessageRequest = {
  messageId: number;
  userId: number;
};

export interface InternalMessageService {
  ListMessage(request: any): Promise<ListInternalMessageResponse>;
  GetMessage(request: GetInternalMessageRequest): Promise<InternalMessage>;
  CreateMessage(request: CreateInternalMessageRequest): Promise<InternalMessage>;
  UpdateMessage(request: UpdateInternalMessageRequest): Promise<wellKnownEmpty>;
  DeleteMessage(request: DeleteInternalMessageRequest): Promise<wellKnownEmpty>;
  SendMessage(request: SendMessageRequest): Promise<SendMessageResponse>;
  RevokeMessage(request: RevokeMessageRequest): Promise<wellKnownEmpty>;
}

export function createInternalMessageServiceClient(
  handler: RequestHandler
): InternalMessageService {
  return {
    ListMessage(request) {
      const path = `v1/internal-message/messages`;
      const body = null;
      const queryParams: string[] = [];
      if (request.page) queryParams.push(`page=${encodeURIComponent(request.page.toString())}`);
      if (request.pageSize) queryParams.push(`pageSize=${encodeURIComponent(request.pageSize.toString())}`);
      if (request.noPaging) queryParams.push(`noPaging=${encodeURIComponent(request.noPaging.toString())}`);
      if (request.query) queryParams.push(`query=${encodeURIComponent(request.query.toString())}`);
      if (request.orderBy) queryParams.push(`orderBy=${encodeURIComponent(request.orderBy.toString())}`);
      if (request.fieldMask) queryParams.push(`fieldMask=${encodeURIComponent(request.fieldMask.toString())}`);
      let uri = path;
      if (queryParams.length > 0) uri += `?${queryParams.join("&")}`;
      return handler({ path: uri, method: "GET", body }, {
        service: "InternalMessageService", method: "ListMessage",
      }) as Promise<ListInternalMessageResponse>;
    },
    GetMessage(request) {
      if (!request.id) throw new Error("missing required field request.id");
      const path = `v1/internal-message/messages/${request.id}`;
      return handler({ path, method: "GET", body: null }, {
        service: "InternalMessageService", method: "GetMessage",
      }) as Promise<InternalMessage>;
    },
    CreateMessage(request) {
      const path = `v1/internal-message/messages`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageService", method: "CreateMessage",
      }) as Promise<InternalMessage>;
    },
    UpdateMessage(request) {
      if (!request.id) throw new Error("missing required field request.id");
      const path = `v1/internal-message/messages/${request.id}`;
      const body = JSON.stringify(request);
      return handler({ path, method: "PUT", body }, {
        service: "InternalMessageService", method: "UpdateMessage",
      }) as Promise<wellKnownEmpty>;
    },
    DeleteMessage(request) {
      if (!request.id) throw new Error("missing required field request.id");
      const path = `v1/internal-message/messages/${request.id}`;
      return handler({ path, method: "DELETE", body: null }, {
        service: "InternalMessageService", method: "DeleteMessage",
      }) as Promise<wellKnownEmpty>;
    },
    SendMessage(request) {
      const path = `v1/internal-message/send`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageService", method: "SendMessage",
      }) as Promise<SendMessageResponse>;
    },
    RevokeMessage(request) {
      const path = `v1/internal-message/revoke`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageService", method: "RevokeMessage",
      }) as Promise<wellKnownEmpty>;
    },
  };
}

// ---------- Internal Message Recipient ----------

export type InternalMessageRecipient_Status =
  | "SENT"
  | "RECEIVED"
  | "READ"
  | "REVOKED"
  | "DELETED";

export type InternalMessageRecipient = {
  id?: number;
  messageId?: number;
  recipientUserId?: number;
  status?: InternalMessageRecipient_Status;
  receivedAt?: wellKnownTimestamp;
  readAt?: wellKnownTimestamp;
  title?: string;
  content?: string;
  tenantId?: number;
  tenantName?: string;
  createdBy?: number;
  updatedBy?: number;
  deletedBy?: number;
  createdAt?: wellKnownTimestamp;
  updatedAt?: wellKnownTimestamp;
  deletedAt?: wellKnownTimestamp;
};

export type ListInternalMessageRecipientResponse = {
  items: InternalMessageRecipient[] | undefined;
  total: number | undefined;
};

export type ListUserInboxResponse = {
  items: InternalMessageRecipient[] | undefined;
  total: number | undefined;
};

export type MarkNotificationAsReadRequest = {
  userId: number;
  recipientIds: number[];
};

export type DeleteNotificationFromInboxRequest = {
  userId: number;
  recipientIds: number[];
};

export type MarkNotificationsStatusRequest = {
  userId: number;
  recipientIds: number[];
  newStatus: InternalMessageRecipient_Status;
};

export interface InternalMessageRecipientService {
  ListUserInbox(request: any): Promise<ListUserInboxResponse>;
  MarkNotificationAsRead(request: MarkNotificationAsReadRequest): Promise<wellKnownEmpty>;
  DeleteNotificationFromInbox(request: DeleteNotificationFromInboxRequest): Promise<wellKnownEmpty>;
  MarkNotificationsStatus(request: MarkNotificationsStatusRequest): Promise<wellKnownEmpty>;
}

export function createInternalMessageRecipientServiceClient(
  handler: RequestHandler
): InternalMessageRecipientService {
  return {
    ListUserInbox(request) {
      const path = `v1/internal-message/inbox`;
      const body = null;
      const queryParams: string[] = [];
      if (request.page) queryParams.push(`page=${encodeURIComponent(request.page.toString())}`);
      if (request.pageSize) queryParams.push(`pageSize=${encodeURIComponent(request.pageSize.toString())}`);
      if (request.noPaging) queryParams.push(`noPaging=${encodeURIComponent(request.noPaging.toString())}`);
      if (request.query) queryParams.push(`query=${encodeURIComponent(request.query.toString())}`);
      if (request.orderBy) queryParams.push(`orderBy=${encodeURIComponent(request.orderBy.toString())}`);
      if (request.fieldMask) queryParams.push(`fieldMask=${encodeURIComponent(request.fieldMask.toString())}`);
      let uri = path;
      if (queryParams.length > 0) uri += `?${queryParams.join("&")}`;
      return handler({ path: uri, method: "GET", body }, {
        service: "InternalMessageRecipientService", method: "ListUserInbox",
      }) as Promise<ListUserInboxResponse>;
    },
    MarkNotificationAsRead(request) {
      const path = `v1/internal-message/inbox/read`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageRecipientService", method: "MarkNotificationAsRead",
      }) as Promise<wellKnownEmpty>;
    },
    DeleteNotificationFromInbox(request) {
      const path = `v1/internal-message/inbox/delete`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageRecipientService", method: "DeleteNotificationFromInbox",
      }) as Promise<wellKnownEmpty>;
    },
    MarkNotificationsStatus(request) {
      const path = `v1/internal-message/inbox/status`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageRecipientService", method: "MarkNotificationsStatus",
      }) as Promise<wellKnownEmpty>;
    },
  };
}

// ---------- Internal Message Category ----------

export type InternalMessageCategory = {
  id?: number;
  name?: string;
  code?: string;
  iconUrl?: string;
  sortOrder?: number;
  isEnabled?: boolean;
  tenantId?: number;
  tenantName?: string;
  createdBy?: number;
  updatedBy?: number;
  deletedBy?: number;
  createdAt?: wellKnownTimestamp;
  updatedAt?: wellKnownTimestamp;
  deletedAt?: wellKnownTimestamp;
};

export type ListInternalMessageCategoryResponse = {
  items: InternalMessageCategory[] | undefined;
  total: number | undefined;
};

export type GetInternalMessageCategoryRequest = {
  id: number;
};

export type CreateInternalMessageCategoryRequest = {
  data: Partial<InternalMessageCategory>;
};

export type UpdateInternalMessageCategoryRequest = {
  id: number;
  data: Partial<InternalMessageCategory>;
  updateMask?: { paths: string[] };
  allowMissing?: boolean;
};

export type DeleteInternalMessageCategoryRequest = {
  id: number;
};

export interface InternalMessageCategoryService {
  List(request: any): Promise<ListInternalMessageCategoryResponse>;
  Get(request: GetInternalMessageCategoryRequest): Promise<InternalMessageCategory>;
  Create(request: CreateInternalMessageCategoryRequest): Promise<wellKnownEmpty>;
  Update(request: UpdateInternalMessageCategoryRequest): Promise<wellKnownEmpty>;
  Delete(request: DeleteInternalMessageCategoryRequest): Promise<wellKnownEmpty>;
}

export function createInternalMessageCategoryServiceClient(
  handler: RequestHandler
): InternalMessageCategoryService {
  return {
    List(request) {
      const path = `v1/internal-message/categories`;
      const body = null;
      const queryParams: string[] = [];
      if (request.page) queryParams.push(`page=${encodeURIComponent(request.page.toString())}`);
      if (request.pageSize) queryParams.push(`pageSize=${encodeURIComponent(request.pageSize.toString())}`);
      if (request.noPaging) queryParams.push(`noPaging=${encodeURIComponent(request.noPaging.toString())}`);
      if (request.query) queryParams.push(`query=${encodeURIComponent(request.query.toString())}`);
      if (request.orderBy) queryParams.push(`orderBy=${encodeURIComponent(request.orderBy.toString())}`);
      if (request.fieldMask) queryParams.push(`fieldMask=${encodeURIComponent(request.fieldMask.toString())}`);
      let uri = path;
      if (queryParams.length > 0) uri += `?${queryParams.join("&")}`;
      return handler({ path: uri, method: "GET", body }, {
        service: "InternalMessageCategoryService", method: "List",
      }) as Promise<ListInternalMessageCategoryResponse>;
    },
    Get(request) {
      if (!request.id) throw new Error("missing required field request.id");
      const path = `v1/internal-message/categories/${request.id}`;
      return handler({ path, method: "GET", body: null }, {
        service: "InternalMessageCategoryService", method: "Get",
      }) as Promise<InternalMessageCategory>;
    },
    Create(request) {
      const path = `v1/internal-message/categories`;
      const body = JSON.stringify(request);
      return handler({ path, method: "POST", body }, {
        service: "InternalMessageCategoryService", method: "Create",
      }) as Promise<wellKnownEmpty>;
    },
    Update(request) {
      if (!request.id) throw new Error("missing required field request.id");
      const path = `v1/internal-message/categories/${request.id}`;
      const body = JSON.stringify(request);
      return handler({ path, method: "PUT", body }, {
        service: "InternalMessageCategoryService", method: "Update",
      }) as Promise<wellKnownEmpty>;
    },
    Delete(request) {
      if (!request.id) throw new Error("missing required field request.id");
      const path = `v1/internal-message/categories/${request.id}`;
      return handler({ path, method: "DELETE", body: null }, {
        service: "InternalMessageCategoryService", method: "Delete",
      }) as Promise<wellKnownEmpty>;
    },
  };
}

// @@protoc_insertion_point(typescript-http-eof)
