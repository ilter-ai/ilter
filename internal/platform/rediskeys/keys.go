// Package rediskeys builds the Redis key strings used for budget, rate-limit,
// and cache entries. It has no dependency on SQLite/sqlc — it is pure string
// formatting shared by the middleware and features packages that talk to Redis.
package rediskeys

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func key(parts ...string) string {
	return strings.Join(parts, ":")
}

func BudgetKey(keyID string, t time.Time) string {
	return key("ilter", "budget", keyID, "monthly", t.Format("2006-01"))
}

func DailyBudgetKey(keyID string, t time.Time) string {
	return key("ilter", "budget", keyID, "daily", t.Format("2006-01-02"))
}

func RateLimitKey(keyID string, t time.Time) string {
	return key("ilter", "ratelimit", keyID, "rpm", fmt.Sprintf("%d", t.Unix()/60))
}

func UserBudgetKey(userID int, t time.Time) string {
	return key("ilter", "budget", "user", fmt.Sprintf("%d", userID), "monthly", t.Format("2006-01"))
}

func UserDailyBudgetKey(userID int, t time.Time) string {
	return key("ilter", "budget", "user", fmt.Sprintf("%d", userID), "daily", t.Format("2006-01-02"))
}

func GroupBudgetKey(groupID int, t time.Time) string {
	return key("ilter", "budget", "group", fmt.Sprintf("%d", groupID), "monthly", t.Format("2006-01"))
}

func GroupDailyBudgetKey(groupID int, t time.Time) string {
	return key("ilter", "budget", "group", fmt.Sprintf("%d", groupID), "daily", t.Format("2006-01-02"))
}

func CacheKey() string {
	return key("ilter", "cache", uuid.New().String())
}

func UserRateLimitCounterKey(userID int, t time.Time) string {
	return key("ilter", "ratelimit", "user", fmt.Sprintf("%d", userID), "rpm", fmt.Sprintf("%d", t.Unix()/60))
}

func GroupRateLimitCounterKey(groupID int, t time.Time) string {
	return key("ilter", "ratelimit", "group", fmt.Sprintf("%d", groupID), "rpm", fmt.Sprintf("%d", t.Unix()/60))
}

func UserRateLimitConfigKey(userID int) string {
	return key("ilter", "ratelimit", "config", "user", fmt.Sprintf("%d", userID))
}

func GroupRateLimitConfigKey(groupID int) string {
	return key("ilter", "ratelimit", "config", "group", fmt.Sprintf("%d", groupID))
}

func UserRateLimitRetryAfterKey(userID int) string {
	return key("ilter", "ratelimit", "config", "user", fmt.Sprintf("%d", userID), "retryafter")
}

func GroupRateLimitRetryAfterKey(groupID int) string {
	return key("ilter", "ratelimit", "config", "group", fmt.Sprintf("%d", groupID), "retryafter")
}
