// Package alerts publishes operational alerts (e.g. "withdrawal stuck, hot
// wallet underfunded") onto the shared platform Redis Stream that the platform
// Telegram bot consumes and posts to the project's topic. It is a producer
// only: Send does one XADD — sub-millisecond on the local Redis — and returns;
// the slow Telegram call happens in the bot, off the processing path. A
// disabled/empty config makes Send a no-op, so a missing alert channel never
// crashes a payment flow.
//
// Every project writes to ONE shared stream in a reserved Redis DB (not its
// own app DB); the bot routes each entry by its `project` field. Call it from
// the code that detects the event, best-effort: log the error and carry on,
// never fail the transaction.
package alerts

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Alert severities. Free-form, but the bot renders these three with an icon.
const (
	LevelInfo = "info"
	LevelWarn = "warn"
	LevelCrit = "crit"
)

const defaultStream = "alerts"

// Config is what the publisher needs; the composition root maps the app config
// into it (kept local so this package doesn't import config).
type Config struct {
	Enabled       bool
	Project       string // slug the bot routes on (this project's name)
	RedisURL      string // shared platform redis, e.g. redis:6379
	RedisPassword string
	RedisDB       int // reserved shared alerts DB (not the app DB)
	Stream        string
}

// Publisher writes alerts for one project onto the shared stream.
type Publisher struct {
	rdb     *redis.Client
	project string
	stream  string
}

// New builds a Publisher. When disabled or missing required fields it returns a
// no-op publisher (Configured() == false, Send returns nil).
func New(cfg Config) *Publisher {
	if !cfg.Enabled || cfg.RedisURL == "" || cfg.Project == "" {
		return &Publisher{}
	}
	stream := cfg.Stream
	if stream == "" {
		stream = defaultStream
	}
	return &Publisher{
		rdb: redis.NewClient(&redis.Options{
			Addr:     cfg.RedisURL,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}),
		project: cfg.Project,
		stream:  stream,
	}
}

// Configured reports whether alerts are wired (else Send is a no-op).
func (p *Publisher) Configured() bool { return p != nil && p.rdb != nil }

// Send publishes one alert. Best-effort — log the error and carry on. The XADD
// is capped (MAXLEN ~) so a stalled bot can't grow Redis unbounded.
func (p *Publisher) Send(ctx context.Context, level, text string) error {
	if !p.Configured() {
		return nil
	}
	return p.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		MaxLen: 10000,
		Approx: true,
		Values: map[string]any{"project": p.project, "level": level, "text": text},
	}).Err()
}

// Close releases the Redis client.
func (p *Publisher) Close() error {
	if !p.Configured() {
		return nil
	}
	return p.rdb.Close()
}
