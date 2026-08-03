package middleware

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/budget"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// BudgetMiddleware is the thin HTTP middleware adapter wrapping budget.Enforcer.
type BudgetMiddleware struct {
	enforcer *budget.Enforcer
}

// NewBudgetMiddleware creates a new BudgetMiddleware adapter.
func NewBudgetMiddleware(cfg config.BudgetConfig, g *circuitbreaker.RedisBreaker, store *db.SQLiteStore, cfgCache *config.Cache) *BudgetMiddleware {
	enforcer := budget.NewEnforcer(cfg, g, store, cfgCache)
	return &BudgetMiddleware{enforcer: enforcer}
}

// Enforcer returns the underlying budget.Enforcer core.
func (m *BudgetMiddleware) Enforcer() *budget.Enforcer {
	return m.enforcer
}

// Handler returns the Chi-compatible HTTP middleware handler.
func (m *BudgetMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enabled := m.enforcer.Cfg.Enabled
		if m.enforcer.CfgCache != nil {
			enabled = config.IsEnabled(m.enforcer.CfgCache, "budget")
		}
		if !enabled {
			next.ServeHTTP(w, r)
			return
		}

		keyID := reqmeta.GetKeyID(r.Context())
		if keyID == "" {
			next.ServeHTTP(w, r)
			return
		}

		meta := reqmeta.GetRequestMetadata(r.Context())

		if m.enforcer.Guard == nil {
			next.ServeHTTP(w, r)
			return
		}

		now := time.Now()

		monthlyLimitMicro := budget.CostToMicrosCeil(m.enforcer.Cfg.DefaultMonthlyLimit)
		if b, ok := reqmeta.GetAPIKeyBudget(r.Context()); ok && b > 0 {
			monthlyLimitMicro = budget.CostToMicrosCeil(b)
		}
		dailyLimitMicro := budget.CostToMicrosCeil(m.enforcer.Cfg.DefaultDailyLimit)
		if d, ok := reqmeta.GetAPIKeyDailyLimit(r.Context()); ok && d > 0 {
			dailyLimitMicro = budget.CostToMicrosCeil(d)
		}

		monthlySpent, dailySpent, monthlyExceeded, dailyExceeded := m.enforcer.ReadBudgetState(r.Context(), keyID, now, monthlyLimitMicro, dailyLimitMicro)

		w.Header().Set("X-Budget-Limit", fmt.Sprintf("%.4f", float64(monthlyLimitMicro)/1_000_000))
		monthlyRemaining := max(monthlyLimitMicro-monthlySpent, 0)
		w.Header().Set("X-Budget-Remaining", fmt.Sprintf("%.4f", float64(monthlyRemaining)/1_000_000))
		w.Header().Set("X-Budget-Daily-Limit", fmt.Sprintf("%.4f", float64(dailyLimitMicro)/1_000_000))
		dailyRemaining := max(dailyLimitMicro-dailySpent, 0)
		w.Header().Set("X-Budget-Daily-Remaining", fmt.Sprintf("%.4f", float64(dailyRemaining)/1_000_000))

		if monthlyExceeded {
			if meta != nil {
				meta.SetBudgetExceeded(true)
			}
			model.WriteJSONError(w, http.StatusTooManyRequests, "budget_exceeded", "Budget limit exceeded")
			return
		}
		if dailyExceeded {
			if meta != nil {
				meta.SetBudgetExceeded(true)
			}
			model.WriteJSONError(w, http.StatusTooManyRequests, "budget_exceeded", "Daily budget limit exceeded")
			return
		}

		if uid := reqmeta.GetUserID(r.Context()); uid != nil {
			userKey := rediskeys.UserBudgetKey(*uid, now)
			userDayKey := rediskeys.UserDailyBudgetKey(*uid, now)
			m.enforcer.Guard.Do(r.Context(), func(cctx context.Context, cl *redis.Client) error {
				if s, err := cl.Get(cctx, userKey).Result(); err == nil {
					if v, pe := strconv.ParseInt(s, 10, 64); pe == nil {
						r := monthlyLimitMicro - v
						if r < monthlyRemaining {
							monthlyRemaining = r
						}
					}
				}
				if s, err := cl.Get(cctx, userDayKey).Result(); err == nil {
					if v, pe := strconv.ParseInt(s, 10, 64); pe == nil && dailyLimitMicro > 0 {
						r := dailyLimitMicro - v
						if r < dailyRemaining {
							dailyRemaining = r
						}
					}
				}
				return nil
			})
		}
		if gids := reqmeta.GetGroupIDs(r.Context()); len(gids) > 0 {
			for _, gid := range gids {
				gKey := rediskeys.GroupBudgetKey(gid, now)
				gDayKey := rediskeys.GroupDailyBudgetKey(gid, now)
				m.enforcer.Guard.Do(r.Context(), func(cctx context.Context, cl *redis.Client) error {
					if s, err := cl.Get(cctx, gKey).Result(); err == nil {
						if v, pe := strconv.ParseInt(s, 10, 64); pe == nil {
							r := monthlyLimitMicro - v
							if r < monthlyRemaining {
								monthlyRemaining = r
							}
						}
					}
					if s, err := cl.Get(cctx, gDayKey).Result(); err == nil {
						if v, pe := strconv.ParseInt(s, 10, 64); pe == nil && dailyLimitMicro > 0 {
							r := dailyLimitMicro - v
							if r < dailyRemaining {
								dailyRemaining = r
							}
						}
					}
					return nil
				})
			}
		}
		w.Header().Set("X-Budget-Remaining", fmt.Sprintf("%.4f", float64(math.Max(0, float64(monthlyRemaining)))/1_000_000))
		w.Header().Set("X-Budget-Daily-Remaining", fmt.Sprintf("%.4f", float64(math.Max(0, float64(dailyRemaining)))/1_000_000))
		if monthlyLimitMicro > 0 && monthlyRemaining <= 0 {
			if meta != nil {
				meta.SetBudgetExceeded(true)
			}
			model.WriteJSONError(w, http.StatusTooManyRequests, "budget_exceeded", "Budget limit exceeded")
			return
		}
		if dailyLimitMicro > 0 && dailyRemaining <= 0 {
			if meta != nil {
				meta.SetBudgetExceeded(true)
			}
			model.WriteJSONError(w, http.StatusTooManyRequests, "budget_exceeded", "Daily budget limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}
