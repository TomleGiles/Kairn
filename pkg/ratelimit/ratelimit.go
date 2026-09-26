// Package ratelimit limite le débit par clé (principal ou IP) avec un seau à
// jetons. Le limiteur Redis/Valkey partage l'état entre les réplicas de l'API
// (sinon chaque réplica accorderait sa propre limite) ; en cas d'indisponibilité
// de Redis, il se replie sur un seau local plutôt que de bloquer l'API.
package ratelimit

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter décide si une requête identifiée par key est autorisée.
type Limiter interface {
	Allow(ctx context.Context, key string) bool
}

// ------------------------------------------------------------------ mémoire

// Memory est un seau à jetons local au processus.
type Memory struct {
	rate, burst float64
	now         func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// maxBuckets borne la mémoire : au-delà, les seaux sont réinitialisés.
const maxBuckets = 100_000

// NewMemory crée un limiteur local : rate jetons par seconde, burst jetons au plus.
func NewMemory(rate, burst float64) *Memory {
	return &Memory{rate: rate, burst: burst, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow implémente Limiter.
func (l *Memory) Allow(_ context.Context, key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			l.buckets = map[string]*bucket{} // purge grossière anti-épuisement mémoire
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ------------------------------------------------------------------ Redis

// script applique le seau à jetons de façon atomique côté Redis.
// KEYS[1] = clé ; ARGV = débit (jetons/s), capacité, maintenant (ms).
var script = redis.NewScript(`
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local data = redis.call('HMGET', KEYS[1], 't', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil or ts == nil then
  tokens = burst
  ts = now
end
tokens = math.min(burst, tokens + math.max(0, now - ts) / 1000 * rate)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HSET', KEYS[1], 't', tostring(tokens), 'ts', tostring(now))
redis.call('PEXPIRE', KEYS[1], math.ceil(burst / rate * 1000) + 1000)
return allowed
`)

// Redis est un limiteur partagé entre réplicas.
type Redis struct {
	client      redis.Scripter
	rate, burst float64
	prefix      string
	fallback    *Memory
	log         *slog.Logger
	now         func() time.Time

	mu        sync.Mutex
	lastWarn  time.Time
	degraded  bool
	callLimit time.Duration
}

// NewRedis crée un limiteur partagé ; les clés sont préfixées par « kairn:rl: ».
func NewRedis(client redis.Scripter, rate, burst float64, log *slog.Logger) *Redis {
	if log == nil {
		log = slog.Default()
	}
	return &Redis{client: client, rate: rate, burst: burst, prefix: "kairn:rl:", fallback: NewMemory(rate, burst),
		log: log, now: time.Now, callLimit: 200 * time.Millisecond}
}

// Allow implémente Limiter. Une erreur Redis bascule sur le seau local.
func (l *Redis) Allow(ctx context.Context, key string) bool {
	ctx, cancel := context.WithTimeout(ctx, l.callLimit)
	defer cancel()
	res, err := script.Run(ctx, l.client, []string{l.prefix + key}, l.rate, l.burst, l.now().UnixMilli()).Int()
	if err != nil {
		l.warn(err)
		return l.fallback.Allow(ctx, key)
	}
	l.recovered()
	return res == 1
}

func (l *Redis) warn(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.degraded = true
	if now := l.now(); now.Sub(l.lastWarn) > time.Minute {
		l.lastWarn = now
		l.log.Warn("rate limiter: redis unavailable, using a local bucket", "err", err)
	}
}

func (l *Redis) recovered() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.degraded {
		l.degraded = false
		l.log.Info("rate limiter: redis available again")
	}
}

// Open renvoie un limiteur Redis si redisURL est renseignée (redis:// ou
// rediss://), sinon un limiteur local. La fonction retournée ferme la connexion.
func Open(redisURL string, rate, burst float64, log *slog.Logger) (Limiter, func(), error) {
	if redisURL == "" {
		return NewMemory(rate, burst), func() {}, nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, nil, err
	}
	client := redis.NewClient(opts)
	return NewRedis(client, rate, burst, log), func() { _ = client.Close() }, nil
}
