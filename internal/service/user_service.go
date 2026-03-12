package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	adminstubpb "github.com/go-tangra/go-tangra-common/gen/go/common/admin_stub/v1"
	"github.com/go-tangra/go-tangra-notification/internal/client"
	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

type UserService struct {
	notificationpb.UnimplementedNotificationUserServiceServer

	log         *log.Helper
	adminClient *client.AdminClient
}

func NewUserService(ctx *bootstrap.Context, adminClient *client.AdminClient) *UserService {
	return &UserService{
		log:         ctx.NewLoggerHelper("notification/service/user"),
		adminClient: adminClient,
	}
}

func (s *UserService) ListUsers(ctx context.Context, req *notificationpb.ListNotificationUsersRequest) (*notificationpb.ListNotificationUsersResponse, error) {
	if getUserIDAsUint32(ctx) == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	resp, err := s.adminClient.ListUsers(ctx)
	if err != nil {
		s.log.Errorf("Failed to list users from admin-service: %v", err)
		return nil, err
	}

	items := make([]*notificationpb.NotificationUser, 0, len(resp.Items))
	for _, u := range resp.Items {
		if u.Status != nil && u.GetStatus() == adminstubpb.AdminUser_PENDING {
			continue
		}
		items = append(items, &notificationpb.NotificationUser{
			Id:       u.Id,
			Username: u.Username,
			Realname: u.Realname,
		})
	}

	return &notificationpb.ListNotificationUsersResponse{
		Items: items,
		Total: int32(len(items)),
	}, nil
}

func (s *UserService) ListRoles(ctx context.Context, req *notificationpb.ListNotificationRolesRequest) (*notificationpb.ListNotificationRolesResponse, error) {
	if getUserIDAsUint32(ctx) == nil {
		return nil, notificationpb.ErrorAccessDenied("authentication required")
	}

	resp, err := s.adminClient.ListRoles(ctx)
	if err != nil {
		s.log.Errorf("Failed to list roles from admin-service: %v", err)
		return nil, err
	}

	items := make([]*notificationpb.NotificationRole, 0, len(resp.Items))
	for _, r := range resp.Items {
		items = append(items, &notificationpb.NotificationRole{
			Id:          r.Id,
			Name:        r.Name,
			Code:        r.Code,
			Description: r.Description,
		})
	}

	return &notificationpb.ListNotificationRolesResponse{
		Items: items,
		Total: int32(len(items)),
	}, nil
}
