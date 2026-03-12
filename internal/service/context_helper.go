package service

import (
	"context"

	"github.com/go-tangra/go-tangra-common/grpcx"
	"github.com/go-tangra/go-tangra-common/middleware/mtls"
)

var (
	getTenantIDFromContext = grpcx.GetTenantIDFromContext
	getUserIDAsUint32     = grpcx.GetUserIDAsUint32
	isPlatformAdmin       = grpcx.IsPlatformAdmin
)

// callerIdentity represents either a user or an mTLS client making the request.
type callerIdentity struct {
	isClient bool   // true if mTLS client, false if user
	userID   uint32 // valid when !isClient
	clientID string // valid when isClient (cert CN, e.g. "lcm-paperless")
}

// getCallerIdentity extracts the caller identity from context.
// It first checks for a user ID from gRPC metadata (gateway-forwarded requests),
// then falls back to the mTLS client certificate CN (service-to-service calls).
func getCallerIdentity(ctx context.Context) *callerIdentity {
	if uid := getUserIDAsUint32(ctx); uid != nil {
		return &callerIdentity{isClient: false, userID: *uid}
	}
	if clientID := mtls.GetClientID(ctx); clientID != "" {
		return &callerIdentity{isClient: true, clientID: clientID}
	}
	return nil
}
