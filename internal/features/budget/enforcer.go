package budget

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"

	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func CostToMicros(cost float64) int64 { return int64(math.Round(cost * 1e6)) }

func CostToMicrosCeil(limit float64) int64 { return int64(math.Ceil(limit * 1e6)) }

type Enforcer struct {
	Cfg      config.BudgetConfig
	Guard    *circuitbreaker.RedisBreaker
	Store    *db.SQLiteStore
	CfgCache *config.Cache
}

func NewEnforcer(cfg config.BudgetConfig, g *circuitbreaker.RedisBreaker, store *db.SQLiteStore, cfgCache *config.Cache) *Enforcer {
	return &Enforcer{
		Cfg:      cfg,
		Guard:    g,
		Store:    store,
		CfgCache: cfgCache,
	}
}

// ReadBudgetState fetches all budget counters from Redis in a single
// guard-wrapped call. Returns the key-level monthly/daily totals and
// whether each limit is exceeded (non-zero limit and spent >= limit).
func (be *Enforcer) ReadBudgetState(ctx context.Context, keyID string, now time.Time, monthlyLimitMicro, dailyLimitMicro int64) (monthlySpentMicro, dailySpentMicro int64, monthlyExceeded, dailyExceeded bool) {
	monthKey := rediskeys.BudgetKey(keyID, now)
	dayKey := rediskeys.DailyBudgetKey(keyID, now)

	// handle both float-dollar and integer-micro formats, matching
	// the Lua ensureMicro() logic in budgetCheckScript below.
	parseMicro := func(s string) int64 {
		if strings.Contains(s, ".") {
			f, err := strconv.ParseFloat(s, 64)
			if err == nil {
				return int64(math.Round(f * 1e6))
			}
		}
		v, _ := strconv.ParseInt(s, 10, 64)
		return v
	}
	be.Guard.Do(ctx, func(cctx context.Context, cl *redis.Client) error {
		if ms, err := cl.Get(cctx, monthKey).Result(); err == nil {
			monthlySpentMicro = parseMicro(ms)
		}
		if ds, err := cl.Get(cctx, dayKey).Result(); err == nil {
			dailySpentMicro = parseMicro(ds)
		}
		return nil // reads are advisory — nil/error are both fine
	})
	monthlyExceeded = monthlyLimitMicro > 0 && monthlySpentMicro >= monthlyLimitMicro
	dailyExceeded = dailyLimitMicro > 0 && dailySpentMicro >= dailyLimitMicro
	return
}

// budgetCheckScript is a Redis Lua script that atomically checks budget limits and
// records usage. This eliminates the TOCTOU race between pre-check and post-deduct.
//
// All values are in micro-dollars (int64, 1 USD = 1,000,000 µUSD).
// Uses INCRBY (integer) instead of INCRBYFLOAT to avoid floating-point accumulation.
//
// **Lazy float→int64 conversion**: existing keys that still hold float-dollar values
// from the pre-migration INCRBYFLOAT era are detected (contains '.') and converted
// to micro-dollars before the INCRBY.  This allows zero-downtime migration —
// keys are converted on first access, one at a time, with no big-bang SCAN.
//
// KEYS[1] = monthly budget key
// KEYS[2] = daily budget key
// ARGV[1] = monthly limit (micro-dollars)
// ARGV[2] = daily limit (micro-dollars, 0 = no daily limit)
// ARGV[3] = cost to add (micro-dollars)
// Returns: "ok" or "exceeded"
const budgetCheckScript = `
local function ensureMicro(key)
	local v = redis.call('GET', key)
	if v and string.find(v, '%.') then
		local micro = math.floor(tonumber(v) * 1000000 + 0.5)
		redis.call('SET', key, micro, 'KEEPTTL')
	end
end

ensureMicro(KEYS[1])
ensureMicro(KEYS[2])

local monthly = redis.call('GET', KEYS[1])
local monthlySpent = 0
if monthly then
	monthlySpent = tonumber(monthly)
end
if monthlySpent + tonumber(ARGV[3]) > tonumber(ARGV[1]) then
	return 'exceeded'
end

if tonumber(ARGV[2]) > 0 then
	local daily = redis.call('GET', KEYS[2])
	local dailySpent = 0
	if daily then
		dailySpent = tonumber(daily)
	end
	if dailySpent + tonumber(ARGV[3]) > tonumber(ARGV[2]) then
		return 'exceeded'
	end
end

redis.call('INCRBY', KEYS[1], ARGV[3])
redis.call('EXPIRE', KEYS[1], 60*24*60)

redis.call('INCRBY', KEYS[2], ARGV[3])
redis.call('EXPIRE', KEYS[2], 2*24*60)

return 'ok'
`

// RecordUsage atomically checks budget limits and records usage via a Redis Lua script.
// This prevents the TOCTOU race where pre-check passes but concurrent requests
// collectively exceed the budget.
//
// cost is in USD dollars — it is converted to micro-dollars (int64) internally
// for integer-arithmetic safety in Redis (INCRBY avoids INCRBYFLOAT precision drift).
func (be *Enforcer) RecordUsage(ctx context.Context, keyID string, cost float64) error {
	enabled := be.Cfg.Enabled
	if be.CfgCache != nil {
		enabled = config.IsEnabled(be.CfgCache, "budget")
	}
	if !enabled || be.Guard == nil || keyID == "" {
		return nil
	}

	now := time.Now()
	monthKey := rediskeys.BudgetKey(keyID, now)
	dayKey := rediskeys.DailyBudgetKey(keyID, now)

	// Use CostToMicros (Round) for unbiased accounting of cost.
	costMicro := CostToMicros(cost)

	// Use CostToMicrosCeil for limits — conservative ceiling prevents
	// rounding down and accidentally allowing over-budget requests.
	monthlyLimitMicro := CostToMicrosCeil(be.Cfg.DefaultMonthlyLimit)
	if b, ok := reqmeta.GetAPIKeyBudget(ctx); ok && b > 0 {
		monthlyLimitMicro = CostToMicrosCeil(b)
	}

	dailyLimitMicro := CostToMicrosCeil(be.Cfg.DefaultDailyLimit)
	if d, ok := reqmeta.GetAPIKeyDailyLimit(ctx); ok && d > 0 {
		dailyLimitMicro = CostToMicrosCeil(d)
	}

	var result string
	degraded := be.Guard.Do(ctx, func(cctx context.Context, cl *redis.Client) error {
		r, err := cl.Eval(cctx, budgetCheckScript, []string{monthKey, dayKey}, monthlyLimitMicro, dailyLimitMicro, costMicro).Result()
		if err != nil {
			return err
		}
		var ok bool
		result, ok = r.(string)
		if !ok {
			slog.Error("budget check: unexpected Redis result type", "type", fmt.Sprintf("%T", r))
			return nil
		}
		return nil
	})
	if degraded {
		return fmt.Errorf("budget check failed: redis unavailable")
	}
	if result == "exceeded" {
		return fmt.Errorf("budget limit exceeded: monthly=%d, daily=%d, cost=%d", monthlyLimitMicro, dailyLimitMicro, costMicro)
	}

	if uid := reqmeta.GetUserID(ctx); uid != nil {
		uMonthKey := rediskeys.UserBudgetKey(*uid, now)
		uDayKey := rediskeys.UserDailyBudgetKey(*uid, now)
		be.Guard.Do(ctx, func(cctx context.Context, cl *redis.Client) error {
			if _, ee := cl.Eval(cctx, budgetCheckScript, []string{uMonthKey, uDayKey}, monthlyLimitMicro, dailyLimitMicro, costMicro).Result(); ee != nil {
				slog.Warn("Failed to record user budget", "user_id", *uid, "error", ee)
			}
			return nil
		})
	}
	if gids := reqmeta.GetGroupIDs(ctx); len(gids) > 0 {
		for _, gid := range gids {
			gMonthKey := rediskeys.GroupBudgetKey(gid, now)
			gDayKey := rediskeys.GroupDailyBudgetKey(gid, now)
			be.Guard.Do(ctx, func(cctx context.Context, cl *redis.Client) error {
				if _, ee := cl.Eval(cctx, budgetCheckScript, []string{gMonthKey, gDayKey}, monthlyLimitMicro, dailyLimitMicro, costMicro).Result(); ee != nil {
					slog.Warn("Failed to record group budget", "group_id", gid, "error", ee)
				}
				return nil
			})
		}
	}

	return nil
}
