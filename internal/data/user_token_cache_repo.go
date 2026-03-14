package data

import (
	"context"
	"fmt"
	"os"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// UserTokenCacheRepo provides read-only access to the portal's user access token cache in Redis.
// The portal writes tokens on login; this service reads them to map user IDs to SSE stream IDs.
type UserTokenCacheRepo struct {
	log *log.Helper
	rdb *redis.Client

	accessTokenKeyPrefix string
}

func NewUserTokenCacheRepo(ctx *bootstrap.Context, rdb *redis.Client) *UserTokenCacheRepo {
	prefix := os.Getenv("ACCESS_TOKEN_KEY_PREFIX")
	if prefix == "" {
		prefix = "uat_"
	}

	return &UserTokenCacheRepo{
		rdb:                  rdb,
		log:                  ctx.NewLoggerHelper("user-token-cache/notification-service"),
		accessTokenKeyPrefix: prefix,
	}
}

// GetAccessTokens returns all active access tokens for a user.
// Each token corresponds to an SSE stream ID.
func (r *UserTokenCacheRepo) GetAccessTokens(ctx context.Context, userId uint32) []string {
	key := fmt.Sprintf("%s%d", r.accessTokenKeyPrefix, userId)

	n, err := r.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		r.log.Warnf("failed to get access tokens for user %d: %v", userId, err)
		return []string{}
	}

	tokens := make([]string, 0, len(n))
	for k := range n {
		tokens = append(tokens, k)
	}

	return tokens
}
