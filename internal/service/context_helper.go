package service

import "github.com/go-tangra/go-tangra-common/grpcx"

var (
	getTenantIDFromContext = grpcx.GetTenantIDFromContext
	getUserIDAsUint32     = grpcx.GetUserIDAsUint32
)
