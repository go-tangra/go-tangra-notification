package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	adminstubpb "github.com/go-tangra/go-tangra-common/gen/go/common/admin_stub/v1"
	"github.com/go-tangra/go-tangra-notification/internal/cert"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
)

// AdminClient calls the admin-service gRPC API for user and role listing
type AdminClient struct {
	log  *log.Helper
	conn *grpc.ClientConn
}

// NewAdminClient creates a new AdminClient connected to admin-service.
// Uses mTLS when certificates are available, falls back to plaintext.
func NewAdminClient(ctx *bootstrap.Context, certManager *cert.CertManager) (*AdminClient, func(), error) {
	l := ctx.NewLoggerHelper("notification/client/admin")

	endpoint := os.Getenv("ADMIN_GRPC_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:7787"
	}

	var transportCreds credentials.TransportCredentials
	if certManager != nil && certManager.IsTLSEnabled() {
		creds, err := loadClientTLS("admin-service", l)
		if err != nil {
			l.Warnf("Failed to load mTLS config for admin client: %v, falling back to plaintext", err)
			transportCreds = insecure.NewCredentials()
		} else {
			transportCreds = creds
			l.Infof("Admin gRPC client configured with mTLS for endpoint: %s", endpoint)
		}
	} else {
		transportCreds = insecure.NewCredentials()
		l.Infof("Admin gRPC client configured for endpoint: %s (plaintext)", endpoint)
	}

	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(transportCreds),
	)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if conn != nil {
			conn.Close()
		}
	}

	return &AdminClient{
		log:  l,
		conn: conn,
	}, cleanup, nil
}

// loadClientTLS loads mTLS credentials for connecting to a target service.
// Convention: CA at {certsDir}/ca/ca.crt, client cert at {certsDir}/notification/notification.{crt,key}
func loadClientTLS(serverName string, l *log.Helper) (credentials.TransportCredentials, error) {
	certsDir := os.Getenv("CERTS_DIR")
	if certsDir == "" {
		certsDir = "/app/certs"
	}

	caCertPath := filepath.Join(certsDir, "ca", "ca.crt")
	clientCertPath := filepath.Join(certsDir, "notification", "notification.crt")
	clientKeyPath := filepath.Join(certsDir, "notification", "notification.key")

	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, os.ErrInvalid
	}

	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS12,
	}

	l.Infof("Loaded TLS credentials: CA=%s, Cert=%s, ServerName=%s", caCertPath, clientCertPath, serverName)
	return credentials.NewTLS(tlsConfig), nil
}

// ListUsers calls admin.service.v1.UserService/List via gRPC
func (c *AdminClient) ListUsers(ctx context.Context) (*adminstubpb.ListAdminUsersResponse, error) {
	noPaging := true
	req := &paginationV1.PagingRequest{NoPaging: &noPaging}

	resp := &adminstubpb.ListAdminUsersResponse{}
	err := c.conn.Invoke(ctx, "/admin.service.v1.UserService/List", req, resp)
	if err != nil {
		c.log.Errorf("Failed to list users from admin-service: %v", err)
		return nil, err
	}

	return resp, nil
}

// ListRoles calls admin.service.v1.RoleService/List via gRPC
func (c *AdminClient) ListRoles(ctx context.Context) (*adminstubpb.ListAdminRolesResponse, error) {
	noPaging := true
	req := &paginationV1.PagingRequest{NoPaging: &noPaging}

	resp := &adminstubpb.ListAdminRolesResponse{}
	err := c.conn.Invoke(ctx, "/admin.service.v1.RoleService/List", req, resp)
	if err != nil {
		c.log.Errorf("Failed to list roles from admin-service: %v", err)
		return nil, err
	}

	return resp, nil
}
