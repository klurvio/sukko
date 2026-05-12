// Command migrate-dlq creates dead-letter Kafka topics for existing active tenants.
// It is a one-shot migration tool to be run once after deploying the routing-rules feature.
//
// Exit codes:
//
//	0 — all tenants processed successfully (or dry-run completed)
//	1 — startup failure (cannot connect to DB or Kafka)
//	2 — partial failure (≥1 tenant DLQ topic creation failed)
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"strconv"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/klurvio/sukko/internal/provisioning"
	"github.com/klurvio/sukko/internal/provisioning/repository"
	kafkautil "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/klurvio/sukko/internal/shared/platform"
	"github.com/klurvio/sukko/internal/shared/routing"
)

const (
	pageSize = 100
)

func main() {
	os.Exit(run())
}

func run() int {
	log := logging.BootstrapLogger("migrate-dlq")

	cfg, err := platform.LoadProvisioningConfig(log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load provisioning config")
	}

	// Flags layer over env vars: CLI flag > KAFKA_BROKERS env var > envDefault.
	dryRun := flag.Bool("dry-run", false, "Log intent without creating topics; exits 0 on success")
	brokerFlag := flag.String("brokers", cfg.KafkaBrokers, "Comma-separated Kafka broker addresses (overrides PROVISIONING_KAFKA_BROKERS)")
	flag.Parse()

	ctx := context.Background()

	pool, err := repository.OpenDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error().Err(err).Msg("Failed to open database")
		return 1
	}
	defer pool.Close()

	brokers := splitBrokers(*brokerFlag)
	kgoClient, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Kafka client")
		return 1
	}
	defer kgoClient.Close()
	admin := kadm.NewClient(kgoClient)

	namespace := kafkautil.ResolveNamespace(cfg.KafkaTopicNamespaceOverride, cfg.Environment)

	tenantRepo := repository.NewTenantRepository(pool)
	statusActive := provisioning.StatusActive
	opts := provisioning.ListOptions{
		Limit:  pageSize,
		Offset: 0,
		Status: &statusActive,
	}

	retentionVal := strconv.FormatInt(cfg.DeadLetterTopicRetentionMs, 10)
	retentionCfg := map[string]*string{
		"retention.ms": &retentionVal,
	}

	var succeeded, failed int

	for {
		tenants, _, err := tenantRepo.List(ctx, opts)
		if err != nil {
			log.Error().Err(err).Int("offset", opts.Offset).Msg("Failed to list tenants")
			return 1
		}
		if len(tenants) == 0 {
			break
		}

		for _, tenant := range tenants {
			dlqTopic := kafkautil.BuildTopicName(namespace, tenant.ID, routing.DeadLetterTopicSuffix)
			if *dryRun {
				log.Info().Str("tenant_id", tenant.ID).Str("topic", dlqTopic).Msg("[dry-run] would create DLQ topic")
				succeeded++
				continue
			}

			resp, err := admin.CreateTopics(ctx,
				int32(cfg.DeadLetterTopicPartitions), //nolint:gosec // G115: partition count validated at config load, always small
				1,
				retentionCfg,
				dlqTopic,
			)
			if err != nil {
				log.Error().Err(err).Str("tenant_id", tenant.ID).Str("topic", dlqTopic).Msg("Failed to create DLQ topic")
				failed++
				continue
			}

			topicErr := resp[dlqTopic].Err
			if topicErr != nil && !isTopicAlreadyExists(topicErr) {
				log.Error().Err(topicErr).Str("tenant_id", tenant.ID).Str("topic", dlqTopic).Msg("Failed to create DLQ topic")
				failed++
				continue
			}

			log.Info().Str("tenant_id", tenant.ID).Str("topic", dlqTopic).Msg("DLQ topic ready")
			succeeded++
		}

		opts.Offset += len(tenants)
		if len(tenants) < pageSize {
			break
		}
	}

	log.Info().
		Int("succeeded", succeeded).
		Int("failed", failed).
		Msg("DLQ migration complete")

	if failed > 0 {
		return 2
	}
	return 0
}

func isTopicAlreadyExists(err error) bool {
	return errors.Is(err, kerr.TopicAlreadyExists)
}

func splitBrokers(brokers string) []string {
	var result []string
	for b := range strings.SplitSeq(brokers, ",") {
		if t := strings.TrimSpace(b); t != "" {
			result = append(result, t)
		}
	}
	return result
}
