package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	commonV1 "github.com/go-tangra/go-tangra-common/gen/go/common/service/v1"
)

// RegisterTasksWithScheduler tells the scheduler which task types the
// notification service can execute. Runs in a background goroutine and
// retries — the scheduler may boot after this service.
//
// Mirrors the registration pattern from go-tangra-backup / go-tangra-lcm.
func RegisterTasksWithScheduler(logger log.Logger) {
	l := log.NewHelper(log.With(logger, "module", "scheduler-registration/notification-service"))

	endpoint := os.Getenv("SCHEDULER_GRPC_ENDPOINT")
	if endpoint == "" {
		l.Info("SCHEDULER_GRPC_ENDPOINT not set, skipping task type registration")
		return
	}

	go func() {
		time.Sleep(10 * time.Second)

		for attempt := 0; attempt < 30; attempt++ {
			if err := doRegisterNotificationTasks(endpoint, l); err != nil {
				l.Warnf("Task type registration attempt %d failed: %v", attempt+1, err)
				time.Sleep(10 * time.Second)
				continue
			}
			return
		}
		l.Error("Failed to register task types with scheduler after 30 attempts")
	}()
}

func doRegisterNotificationTasks(endpoint string, l *log.Helper) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(endpoint, loadSchedulerTLS(l))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := commonV1.NewTaskTypeRegistrationServiceClient(conn)

	resp, err := client.RegisterTaskTypes(ctx, &commonV1.RegisterTaskTypesRequest{
		ModuleId: "notification",
		TaskTypes: []*commonV1.TaskTypeDescriptor{
			{
				TaskType:    "notification:send-test-email",
				DisplayName: "Send test email",
				Description: "Send a test email through the notification service to verify that the configured EMAIL channel is healthy end-to-end.",
				// recipient is required; subject/body default to a
				// recognizable test payload; channelId picks a
				// specific channel if the tenant has more than one.
				PayloadSchema: `{"type":"object","required":["recipient"],"properties":{` +
					`"recipient":{"type":"string","format":"email","description":"Email address to deliver the test message to"},` +
					`"subject":{"type":"string","description":"Optional custom subject line"},` +
					`"body":{"type":"string","description":"Optional custom body (plaintext)"},` +
					`"channelId":{"type":"string","description":"Optional channel UUID; default uses the tenant's default EMAIL channel"}` +
					`}}`,
				// No default cron — this task is typically one-off
				// or runs on demand. Leaving DefaultCron empty
				// keeps the scheduler from creating an automatic
				// daily run.
				DefaultMaxRetry: 1,
			},
		},
	})
	if err != nil {
		return err
	}

	l.Infof("Registered %d task type(s) with scheduler: %s", resp.GetRegisteredCount(), resp.GetMessage())
	return nil
}

// loadSchedulerTLS picks the right client cert for outbound mTLS to the
// scheduler. Mirrors the convention from go-tangra-lcm and go-tangra-
// backup: try the per-module client cert provisioned by LCM bootstrap
// (<CERTS_DIR>/notification/notification.{crt,key}), then a generic
// "client" fallback, then drop to insecure if nothing is available.
func loadSchedulerTLS(l *log.Helper) grpc.DialOption {
	certsDir := os.Getenv("CERTS_DIR")
	if certsDir == "" {
		certsDir = "/app/certs"
	}

	caCert, err := os.ReadFile(filepath.Join(certsDir, "ca", "ca.crt"))
	if err != nil {
		l.Info("No CA cert found, using insecure credentials for scheduler")
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		l.Warn("Failed to parse CA cert, using insecure credentials for scheduler")
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	candidates := [][2]string{
		{filepath.Join(certsDir, "notification", "notification.crt"), filepath.Join(certsDir, "notification", "notification.key")},
		{filepath.Join(certsDir, "client", "client.crt"), filepath.Join(certsDir, "client", "client.key")},
	}
	var clientCert tls.Certificate
	var loaded bool
	for _, pair := range candidates {
		c, err := tls.LoadX509KeyPair(pair[0], pair[1])
		if err == nil {
			clientCert = c
			loaded = true
			l.Infof("Using client cert from %s for scheduler", pair[0])
			break
		}
	}
	if !loaded {
		l.Info("No client cert found in notification/ or client/ — using insecure credentials for scheduler")
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
		ServerName:   "scheduler-service",
		MinVersion:   tls.VersionTLS12,
	}

	l.Info("Using mTLS credentials for scheduler connection")
	return grpc.WithTransportCredentials(credentials.NewTLS(cfg))
}
