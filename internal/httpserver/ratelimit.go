package httpserver

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// tokenLimiter 按令牌维度做每分钟配额的令牌桶限流。
type tokenLimiter struct {
	mu       sync.Mutex
	limiters map[uint64]*rate.Limiter
}

func newTokenLimiter() *tokenLimiter {
	return &tokenLimiter{limiters: map[uint64]*rate.Limiter{}}
}

// allow 判断令牌本次推送是否放行；perMinute 为该令牌的每分钟配额。
func (l *tokenLimiter) allow(tokenID uint64, perMinute int) bool {
	if perMinute < 1 {
		perMinute = 1
	}
	l.mu.Lock()
	lim, ok := l.limiters[tokenID]
	if !ok {
		lim = rate.NewLimiter(rate.Every(time.Minute/time.Duration(perMinute)), perMinute)
		l.limiters[tokenID] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// remove 令牌吊销后清理对应限流器。
func (l *tokenLimiter) remove(tokenID uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.limiters, tokenID)
}
