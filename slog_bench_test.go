package zerolog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// BenchmarkSlogHandler_Simple logs a message with no attributes at all.
func BenchmarkSlogHandler_Simple(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("simple message")
	}
}

// BenchmarkSlogHandler_RecordAttrs logs a message with 5 mixed-type
// attributes passed directly on the call (record attrs), every call.
func BenchmarkSlogHandler_RecordAttrs(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message with attrs",
			"string", "hello world",
			"int", 42,
			"float", 3.14159,
			"bool", true,
			"duration", "1500ms",
		)
	}
}

// BenchmarkSlogHandler_WithAttrs measures the benefit of pre-formatting:
// With(...) is called once during setup, and only the repeated Info calls
// (which reuse the pre-formatted handler) are timed.
func BenchmarkSlogHandler_WithAttrs(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard))).With(
		"service", "checkout",
		"version", "1.4.2",
		"region", "us-east-1",
		"instance_id", 7,
		"ready", true,
	)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message")
	}
}

// BenchmarkSlogHandler_WithGroup logs record attrs nested under a single
// WithGroup, every call.
func BenchmarkSlogHandler_WithGroup(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard))).WithGroup("request")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message",
			"method", "GET",
			"path", "/api/v1/orders",
			"status", 200,
		)
	}
}

// BenchmarkSlogHandler_NestedGroups exercises 3 levels of nested groups,
// each with its own pre-attached attrs, on every call.
func BenchmarkSlogHandler_NestedGroups(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard))).
		WithGroup("request").With("id", "abc123").
		WithGroup("user").With("id", 42).
		WithGroup("session")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "token", "xyz", "expires_in", 3600)
	}
}

// BenchmarkSlogHandler_WithGroupAndAttrs mimics a typical scoped logger:
// a base group + pre-attached attrs set up once, then reused for many
// log calls that each add a couple of their own record attrs.
func BenchmarkSlogHandler_WithGroupAndAttrs(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard))).
		With("service", "checkout").
		WithGroup("request").
		With("id", "abc123", "method", "POST")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "status", 200, "duration_ms", 12)
	}
}

// BenchmarkSlogHandler_Handle drives Handle directly (bypassing the slog.Logger
// convenience layer) with a record built once and reused, isolating handler
// overhead from record construction.
func BenchmarkSlogHandler_Handle(b *testing.B) {
	h := NewSlogHandler(New(io.Discard))
	ctx := context.Background()
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := slog.NewRecord(now, slog.LevelInfo, "message", 0)
		r.AddAttrs(
			slog.String("string", "hello world"),
			slog.Int("int", 42),
			slog.Bool("bool", true),
		)
		_ = h.Handle(ctx, r)
	}
}

// --- KindAny benchmarks -----------------------------------------------
//
// These target slog.KindAny values specifically: attrs whose value isn't
// one of slog's built-in scalar kinds (string/int64/uint64/float64/bool/
// duration/time), so the handler's KindAny switch decides how they're
// encoded. The old handler's KindAny switch only special-cases
// error/time.Duration/time.Time/[]byte and falls back to Event.Interface
// (reflection + json.Marshal) for everything else. The new handler adds
// native, allocation-free zerolog encoders for many more concrete types
// (net.IP, slices of ints/strings/floats/bools/errors, etc.), avoiding
// that reflection path entirely for those types.

// BenchmarkSlogHandler_KindAny_AlreadyCovered uses types BOTH handlers
// already special-case natively (error, time.Duration, []byte). This is
// the control: it should show little to no difference, since the KindAny
// expansion in the new handler doesn't change these paths.
func BenchmarkSlogHandler_KindAny_AlreadyCovered(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	err := errors.New("boom")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message",
			"error", err,
			"duration", 1500*time.Millisecond,
			"payload", []byte("hello world"),
		)
	}
}

// BenchmarkSlogHandler_KindAny_NetIP logs a single net.IP value, a type
// the new handler encodes natively (event.IPAddr) but the old handler
// falls back to Event.Interface (json.Marshal) for.
func BenchmarkSlogHandler_KindAny_NetIP(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	ip := net.IPv4(192, 168, 0, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "client_ip", ip)
	}
}

// BenchmarkSlogHandler_KindAny_StringSlice logs a []string, natively
// encoded via event.Strs in the new handler.
func BenchmarkSlogHandler_KindAny_StringSlice(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	tags := []string{"prod", "us-east-1", "checkout", "critical", "v2"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "tags", tags)
	}
}

// BenchmarkSlogHandler_KindAny_IntSlice logs a []int, natively encoded
// via event.Ints in the new handler.
func BenchmarkSlogHandler_KindAny_IntSlice(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	ids := []int{101, 202, 303, 404, 505}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "order_ids", ids)
	}
}

// BenchmarkSlogHandler_KindAny_ErrorSlice logs a []error, natively
// encoded via event.Errs in the new handler.
func BenchmarkSlogHandler_KindAny_ErrorSlice(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	errs := []error{errors.New("timeout"), errors.New("retry failed"), errors.New("circuit open")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message", "errors", errs)
	}
}

// BenchmarkSlogHandler_KindAny_Mixed combines several of the newly
// expanded KindAny types in a single realistic log call, mimicking a
// request log line with a client IP, response header values, and
// downstream error list all at once.
func BenchmarkSlogHandler_KindAny_Mixed(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard)))
	ip := net.IPv4(10, 0, 0, 5)
	tags := []string{"prod", "checkout"}
	codes := []int{200, 304}
	errs := []error{errors.New("cache miss")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("message",
			"client_ip", ip,
			"tags", tags,
			"status_codes", codes,
			"warnings", errs,
		)
	}
}

// BenchmarkSlogHandler_Parallel logs concurrently from multiple goroutines,
// exercising any shared-state contention in the handler.
func BenchmarkSlogHandler_Parallel(b *testing.B) {
	logger := slog.New(NewSlogHandler(New(io.Discard))).
		With("service", "checkout").
		WithGroup("request")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logger.Info("message", "status", 200, "id", "abc123")
		}
	})
}
