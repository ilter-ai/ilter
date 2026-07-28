package loopdetect

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

type Detector struct {
	mu            sync.RWMutex
	settings      config.LoopSettingsConfig
	requestTimes  map[string][]time.Time
	recentHashes  map[string][]string
	costHistory   map[string][]costEntry
	sessionCounts map[string]sessionEntry
	delayCounts   map[string]int
}

type costEntry struct {
	time time.Time
	cost float64
}

type sessionEntry struct {
	count      int
	lastActive time.Time
}

type CheckResult struct {
	ActiveSignals int
	Warning       bool
	Blocked       bool
	Delay         time.Duration
	PromptHash    string
	RepeatCount   int
	WindowSeconds int
}

func withDefaults(settings config.LoopSettingsConfig) config.LoopSettingsConfig {
	if settings.RateThreshold <= 0 {
		settings.RateThreshold = 30
	}
	if settings.FingerprintWindow <= 0 {
		settings.FingerprintWindow = 20
	}
	if settings.FingerprintDuplicates <= 0 {
		settings.FingerprintDuplicates = 5
	}
	if settings.CostWindow <= 0 {
		settings.CostWindow = 5 * time.Minute
	}
	if settings.CostThreshold <= 0 {
		settings.CostThreshold = 5.0
	}
	if settings.SessionMaxRequests <= 0 {
		settings.SessionMaxRequests = 100
	}
	return settings
}

func NewDetector(settings config.LoopSettingsConfig) *Detector {
	return &Detector{
		settings:      withDefaults(settings),
		requestTimes:  make(map[string][]time.Time),
		recentHashes:  make(map[string][]string),
		costHistory:   make(map[string][]costEntry),
		sessionCounts: make(map[string]sessionEntry),
		delayCounts:   make(map[string]int),
	}
}

// UpdateSettings swaps in new thresholds live — called when the dashboard
// saves Loop Detection settings, so changes take effect immediately instead
// of only on next process restart.
func (d *Detector) UpdateSettings(settings config.LoopSettingsConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.settings = withDefaults(settings)
}

func hashMessages(messages []model.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	lastMsg := messages[len(messages)-1]
	bytes, err := json.Marshal(lastMsg)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(bytes)
	return fmt.Sprintf("%x", hash), nil
}

func (d *Detector) Check(keyID string, sessionID string, messages []model.Message) (CheckResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	reqWindowStart := now.Add(-1 * time.Minute)
	times := d.requestTimes[keyID]
	var validTimes []time.Time
	for _, t := range times {
		if t.After(reqWindowStart) {
			validTimes = append(validTimes, t)
		}
	}
	validTimes = append(validTimes, now)
	d.requestTimes[keyID] = validTimes

	sigRate := len(validTimes) >= d.settings.RateThreshold

	var sigFingerprint bool
	var currentHash string
	var repeatCount int
	if len(messages) > 0 {
		var err error
		currentHash, err = hashMessages(messages)
		if err != nil {
			slog.Error("Failed to hash messages for loop detector", "error", err)
		} else {
			hashes := d.recentHashes[keyID]
			hashes = append(hashes, currentHash)
			if len(hashes) > d.settings.FingerprintWindow {
				hashes = hashes[len(hashes)-d.settings.FingerprintWindow:]
			}
			d.recentHashes[keyID] = hashes

			occurrenceCount := 0
			for _, h := range hashes {
				if h == currentHash {
					occurrenceCount++
				}
			}
			repeatCount = occurrenceCount

			sigFingerprint = occurrenceCount >= d.settings.FingerprintDuplicates
		}
	}

	costWindowStart := now.Add(-d.settings.CostWindow)
	costs := d.costHistory[keyID]
	var validCosts []costEntry
	var totalCost float64
	for _, c := range costs {
		if c.time.After(costWindowStart) {
			validCosts = append(validCosts, c)
			totalCost += c.cost
		}
	}
	d.costHistory[keyID] = validCosts
	sigCost := totalCost >= d.settings.CostThreshold

	var sigSession bool
	if sessionID != "" {
		sEntry := d.sessionCounts[sessionID]
		sEntry.count++
		sEntry.lastActive = now
		d.sessionCounts[sessionID] = sEntry

		sigSession = sEntry.count > d.settings.SessionMaxRequests
	}

	activeSignals := 0
	if sigRate {
		activeSignals++
	}
	if sigFingerprint {
		activeSignals++
	}
	if sigCost {
		activeSignals++
	}
	if sigSession {
		activeSignals++
	}

	windowSecs := int(d.settings.CostWindow / time.Second)
	result := CheckResult{
		ActiveSignals: activeSignals,
		PromptHash:    currentHash,
		RepeatCount:   repeatCount,
		WindowSeconds: windowSecs,
	}

	if activeSignals >= 1 {
		result.Warning = true
	}

	if activeSignals == 2 {
		d.delayCounts[keyID]++
		delayCount := d.delayCounts[keyID]
		sec := min(1<<(delayCount-1), 16)
		result.Delay = time.Duration(sec) * time.Second
	} else if activeSignals < 2 {
		d.delayCounts[keyID] = 0
	}

	if activeSignals >= 3 {
		result.Blocked = true
	}

	if activeSignals > 0 {
		slog.Warn(
			"Loop detector active signals",
			"key_id", keyID,
			"session_id", sessionID,
			"active_signals", activeSignals,
			"sig_rate_anomaly", sigRate,
			"sig_fingerprint_repetition", sigFingerprint,
			"sig_cost_accumulator", sigCost,
			"sig_session_counter", sigSession,
			"delay", result.Delay,
			"blocked", result.Blocked,
		)
	}

	return result, nil
}

func (d *Detector) RecordCost(keyID string, cost float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	d.costHistory[keyID] = append(d.costHistory[keyID], costEntry{
		time: now,
		cost: cost,
	})
}
