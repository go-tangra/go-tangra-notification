package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	commonV1 "github.com/go-tangra/go-tangra-common/gen/go/common/service/v1"
	appViewer "github.com/go-tangra/go-tangra-common/viewer"

	"github.com/go-tangra/go-tangra-notification/internal/data"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/channel"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/notificationlog"
	channelPkg "github.com/go-tangra/go-tangra-notification/pkg/channel"
)

// TaskExecutor implements common.service.v1.TaskExecutorService and handles
// every notification:* task type the scheduler can fire. Today only the
// "notification:send-test-email" task is supported.
type TaskExecutor struct {
	commonV1.UnimplementedTaskExecutorServiceServer

	log          *log.Helper
	channelRepo  *data.ChannelRepo
	notifLogRepo *data.NotificationLogRepo
}

func NewTaskExecutor(
	ctx *bootstrap.Context,
	channelRepo *data.ChannelRepo,
	notifLogRepo *data.NotificationLogRepo,
) *TaskExecutor {
	return &TaskExecutor{
		log:          ctx.NewLoggerHelper("task-executor/notification-service"),
		channelRepo:  channelRepo,
		notifLogRepo: notifLogRepo,
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
	parsedAddr, err := mail.ParseAddress(cfg.Recipient)
	if err != nil {
		return &commonV1.ExecuteTaskResponse{
			Success:          false,
			PermanentFailure: true,
			Message:          fmt.Sprintf("invalid recipient %q: %v", cfg.Recipient, err),
		}, nil
	}
	recipient := parsedAddr.Address

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
	// viewer for ent privacy bypass — same pattern the LCM task
	// executor uses.
	sysCtx := appViewer.NewSystemViewerContext(ctx)

	// Pick the channel. Two paths: explicit channel ID from the
	// payload, or the tenant's default EMAIL channel.
	var ch *ent.Channel
	tenantID := req.GetTenantId()
	if cfg.ChannelID != "" {
		entry, lookupErr := e.channelRepo.GetByID(sysCtx, tenantID, cfg.ChannelID)
		if lookupErr != nil {
			return &commonV1.ExecuteTaskResponse{
				Success: false,
				Message: fmt.Sprintf("channel lookup failed: %v", lookupErr),
			}, nil
		}
		if entry == nil {
			return &commonV1.ExecuteTaskResponse{
				Success:          false,
				PermanentFailure: true,
				Message:          fmt.Sprintf("channel %s not found for tenant %d", cfg.ChannelID, tenantID),
			}, nil
		}
		ch = entry
	} else {
		entry, lookupErr := e.channelRepo.GetDefaultByType(sysCtx, tenantID, channel.TypeEMAIL)
		if lookupErr != nil {
			return &commonV1.ExecuteTaskResponse{
				Success: false,
				Message: fmt.Sprintf("default email channel lookup failed: %v", lookupErr),
			}, nil
		}
		if entry == nil {
			return &commonV1.ExecuteTaskResponse{
				Success:          false,
				PermanentFailure: true,
				Message:          fmt.Sprintf("no default EMAIL channel configured for tenant %d — create one in the notification module first", tenantID),
			}, nil
		}
		ch = entry
	}

	if ch.Type != channel.TypeEMAIL {
		return &commonV1.ExecuteTaskResponse{
			Success:          false,
			PermanentFailure: true,
			Message:          fmt.Sprintf("channel %s is type %s, not EMAIL", ch.ID, ch.Type),
		}, nil
	}
	if !ch.Enabled {
		return &commonV1.ExecuteTaskResponse{
			Success:          false,
			PermanentFailure: true,
			Message:          fmt.Sprintf("channel %s is disabled", ch.ID),
		}, nil
	}

	sender, err := channelPkg.NewEmailSender(ch.Config)
	if err != nil {
		return &commonV1.ExecuteTaskResponse{
			Success: false,
			Message: fmt.Sprintf("failed to build sender for channel %s: %v", ch.ID, err),
		}, nil
	}

	// Record a pending log row BEFORE the SMTP send so the
	// notifications-log page shows scheduler-driven sends alongside
	// regular ones — operators kept asking why the test emails
	// landed in their inbox but were missing from the UI. The log
	// row is then transitioned to SENT / FAILED based on outcome.
	//
	// template_id is empty here: this is a template-less send. The
	// schema accepts empty for exactly this use case (system task
	// executor sends + future ad-hoc internal flows).
	var logEntry *ent.NotificationLog
	if e.notifLogRepo != nil {
		entry, logErr := e.notifLogRepo.Create(
			sysCtx,
			tenantID,
			ch.ID,
			notificationlog.ChannelTypeEMAIL,
			"", // no template — body supplied directly
			recipient,
			cfg.Subject,
			cfg.Body,
			nil, // no created_by user
		)
		if logErr != nil {
			// Don't fail the whole task just because we couldn't
			// write to the log table. The email send is still the
			// primary effect; missing audit row is worth a WARN.
			e.log.Warnf("Failed to create notification log row for test-email task: %v", logErr)
		} else {
			logEntry = entry
		}
	}

	sendErr := sender.Send(ctx, &channelPkg.Message{
		Recipient: recipient,
		Subject:   cfg.Subject,
		Body:      cfg.Body,
	})

	if sendErr != nil {
		if logEntry != nil {
			if _, markErr := e.notifLogRepo.MarkFailed(sysCtx, tenantID, logEntry.ID, sendErr.Error()); markErr != nil {
				e.log.Warnf("Failed to mark notification log row %s as failed: %v", logEntry.ID, markErr)
			}
		}
		// Not flagging as permanent — SMTP outages are usually
		// transient and the scheduler's retry logic is the right
		// place to handle that.
		return &commonV1.ExecuteTaskResponse{
			Success: false,
			Message: fmt.Sprintf("send to %s via channel %s failed: %v", recipient, ch.ID, sendErr),
		}, nil
	}

	if logEntry != nil {
		if _, markErr := e.notifLogRepo.MarkSent(sysCtx, tenantID, logEntry.ID); markErr != nil {
			e.log.Warnf("Failed to mark notification log row %s as sent: %v", logEntry.ID, markErr)
		}
	}

	msg := fmt.Sprintf("Test email sent to %s via channel %s", recipient, ch.ID)
	e.log.Info(msg)
	return &commonV1.ExecuteTaskResponse{
		Success: true,
		Message: msg,
	}, nil
}

