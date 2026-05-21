package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	commonV1 "github.com/go-tangra/go-tangra-common/gen/go/common/service/v1"
	appViewer "github.com/go-tangra/go-tangra-common/viewer"

	notificationpb "github.com/go-tangra/go-tangra-notification/gen/go/notification/service/v1"
)

// TaskExecutor implements common.service.v1.TaskExecutorService and handles
// every notification:* task type the scheduler can fire. Today only the
// "notification:send-test-email" task is supported.
//
// The executor delegates to NotificationService.SendInternal for the
// actual send so that channel resolution, recipient validation, rate
// limiting, log-row management, and metrics all flow through the
// single canonical pipeline used by SendNotification. This keeps the
// /notification/logs view consistent across user-triggered and
// system-triggered sends and ensures future channel features (e.g.
// custom_headers) automatically apply to scheduler-fired tasks.
type TaskExecutor struct {
	commonV1.UnimplementedTaskExecutorServiceServer

	log      *log.Helper
	notifSvc *NotificationService
}

func NewTaskExecutor(ctx *bootstrap.Context, notifSvc *NotificationService) *TaskExecutor {
	return &TaskExecutor{
		log:      ctx.NewLoggerHelper("task-executor/notification-service"),
		notifSvc: notifSvc,
	}
}

// ExecuteTask is the entry point the scheduler calls via gRPC.
func (e *TaskExecutor) ExecuteTask(
	ctx context.Context,
	req *commonV1.ExecuteTaskRequest,
) (*commonV1.ExecuteTaskResponse, error) {
	e.log.Infof("Executing task %s (execution=%s, attempt=%d/%d, tenant=%d)",
		req.GetTaskType(), req.GetExecutionId(), req.GetAttempt(), req.GetMaxAttempts(), req.GetTenantId())

	switch req.GetTaskType() {
	case "notification:send-test-email":
		return e.handleSendTestEmail(ctx, req)
	default:
		return &commonV1.ExecuteTaskResponse{
			Success:          false,
			PermanentFailure: true,
			Message:          fmt.Sprintf("unknown task type: %s", req.GetTaskType()),
		}, nil
	}
}

// SendTestEmailConfig is the payload schema for notification:send-test-email.
type SendTestEmailConfig struct {
	// Email address to deliver the test message to. Required.
	Recipient string `json:"recipient"`
	// Optional subject. Defaults to a recognizable test subject.
	Subject string `json:"subject,omitempty"`
	// Optional body. Defaults to a short plaintext stub naming the
	// execution ID + timestamp so operators can correlate the
	// received email back to the scheduler run.
	Body string `json:"body,omitempty"`
	// Optional channel ID to send through. If unset, uses the
	// tenant's default EMAIL channel.
	ChannelID string `json:"channelId,omitempty"`
}

func (e *TaskExecutor) handleSendTestEmail(
	ctx context.Context,
	req *commonV1.ExecuteTaskRequest,
) (*commonV1.ExecuteTaskResponse, error) {
	cfg := SendTestEmailConfig{}
	if len(req.GetPayload()) > 0 {
		if err := json.Unmarshal(req.GetPayload(), &cfg); err != nil {
			return &commonV1.ExecuteTaskResponse{
				Success:          false,
				PermanentFailure: true,
				Message:          fmt.Sprintf("invalid payload: %v", err),
			}, nil
		}
	}

	if cfg.Recipient == "" {
		return &commonV1.ExecuteTaskResponse{
			Success:          false,
			PermanentFailure: true,
			Message:          `payload missing required field "recipient" (email address)`,
		}, nil
	}

	if cfg.Subject == "" {
		cfg.Subject = "[GoTangra] Scheduled test email"
	}
	if cfg.Body == "" {
		cfg.Body = fmt.Sprintf(
			"This is a scheduled test email from the GoTangra notification service.\r\n"+
				"\r\n"+
				"Execution ID: %s\r\n"+
				"Sent at:      %s\r\n"+
				"\r\n"+
				"If you received this message, your email channel is configured correctly.\r\n",
			req.GetExecutionId(), time.Now().UTC().Format(time.RFC3339),
		)
	}

	// Tasks run without a user identity, so we must elevate to system
	// viewer for ent privacy bypass. SendInternal handles auth/
	// authorization separately (skipped because it's an in-process
	// system caller), but the ent layer still consults the viewer
	// context for tenant-scoped queries.
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	resp, err := e.notifSvc.SendInternal(sysCtx, &InternalSendRequest{
		TenantID:  req.GetTenantId(),
		ChannelID: cfg.ChannelID,
		Recipient: cfg.Recipient,
		Subject:   cfg.Subject,
		Body:      cfg.Body,
		// createdBy stays nil — there's no user behind a scheduler
		// task. The audit row records NULL there; platform admins
		// see the row via the carve-out in ListNotifications.
	})
	if err != nil {
		// gRPC-style errors from validation (e.g. channel disabled,
		// invalid recipient) — surface verbatim so the operator
		// sees the same message they'd see calling SendNotification.
		return &commonV1.ExecuteTaskResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	// SendInternal returns a NotificationLog proto regardless of
	// final status — we look at the audit row's status to decide
	// success vs failure on the scheduler side.
	notif := resp.GetNotification()
	if notif != nil && notif.GetStatus() != notificationpb.DeliveryStatus_DELIVERY_STATUS_SENT {
		return &commonV1.ExecuteTaskResponse{
			Success: false,
			Message: fmt.Sprintf("send failed: %s", notif.GetErrorMessage()),
		}, nil
	}

	msg := fmt.Sprintf("Test email sent to %s", cfg.Recipient)
	e.log.Info(msg)
	return &commonV1.ExecuteTaskResponse{
		Success: true,
		Message: msg,
	}, nil
}
