// Package grpcserver provides the gRPC server for provisioning internal streaming.
package grpcserver

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/klurvio/sukko/internal/shared/logging"
	pkgmetrics "github.com/klurvio/sukko/internal/shared/metrics"
)

// Prometheus metrics for gRPC.
var (
	grpcRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "provisioning_grpc_request_duration_seconds",
		Help:    "Duration of gRPC requests in seconds",
		Buckets: pkgmetrics.APILatencyBuckets,
	}, []string{"method"})

	grpcRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "provisioning_grpc_requests_total",
		Help: "Total number of gRPC requests",
	}, []string{"method", "status"})
)

// RecoveryStreamInterceptor catches panics in stream handlers and returns
// codes.Internal. Uses logging.RecoverPanic pattern per Constitution V.
func RecoveryStreamInterceptor(logger zerolog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logging.RecoverPanic(logger, "grpc_stream_"+info.FullMethod, map[string]any{
					"method": info.FullMethod,
				})
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()

		return handler(srv, ss)
	}
}

// LoggingStreamInterceptor logs gRPC stream lifecycle with method name,
// duration, and status code using zerolog.
func LoggingStreamInterceptor(logger zerolog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		logger.Info().
			Str("method", info.FullMethod).
			Msg("gRPC stream started")

		err := handler(srv, ss)

		duration := time.Since(start)
		code := status.Code(err)

		event := logger.Info()
		if err != nil {
			event = logger.Warn().Err(err)
		}

		event.
			Str("method", info.FullMethod).
			Dur("duration", duration).
			Str("status", code.String()).
			Msg("gRPC stream ended")

		return err
	}
}

// MetricsStreamInterceptor records Prometheus metrics for gRPC streams:
// request duration histogram and request counter by method and status.
func MetricsStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		err := handler(srv, ss)

		duration := time.Since(start)
		code := status.Code(err)

		grpcRequestDuration.WithLabelValues(info.FullMethod).Observe(duration.Seconds())
		grpcRequestsTotal.WithLabelValues(info.FullMethod, code.String()).Inc()

		return err
	}
}
