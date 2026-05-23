// Command migrate-dlq creates the DLQ and default Kafka topics for existing active tenants.
// It is a one-shot migration tool to be run BEFORE upgrading to the channel-topic routing
// release so that infrastructure topics are in place when the new server starts routing.
//
// Exit codes:
//
//	0 — all tenants processed successfully (or dry-run completed)
//	1 — startup failure (cannot connect to DB or Kafka)
//	2 — partial failure (≥1 tenant topic creation failed)
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
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

// topicAdmin is the subset of kadm.Client used by provisionTenantTopics.
// Extracted as an interface so tests can inject a fake without a live broker.
type topicAdmin interface {
	CreateTopics(ctx context.Context, partitions int32, replicationFactor int16, configs map[string]*string, topics ...string) (kadm.CreateTopicResponses, error)
	DeleteTopics(ctx context.Context, topics ...string) (kadm.DeleteTopicResponses, error)
}

func main() {
	os.Exit(run())
}

func run() int {
	log := logging.BootstrapLogger("migrate-dlq")

	cfg, err := platform.LoadProvisioningConfig(log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load provisioning config")
	}

	// Flags layer over env vars: CLI flag > env var > envDefault.
	dryRun := flag.Bool("dry-run", false, "Log intent without creating or deleting topics; exits 0 on success")
	recreate := flag.Bool("recreate", false, "Delete existing topics before recreating (non-atomic: topic may be absent if create fails; re-run to restore)")
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
	rf := int16(cfg.InfraTopicReplicationFactor) //nolint:gosec // G115: RF validated 1–32767 at startup; cannot overflow int16

	dlqRetentionVal := strconv.FormatInt(cfg.DeadLetterTopicRetentionMs, 10)
	defaultRetentionVal := strconv.FormatInt(cfg.DefaultRetentionMs, 10)
	specs := []topicMigSpec{
		{
			suffix:     routing.DeadLetterTopicSuffix,
			partitions: int32(cfg.DeadLetterTopicPartitions), //nolint:gosec // G115: validated >= 1 at startup
			cfg:        map[string]*string{kafkautil.RetentionMsConfigKey: &dlqRetentionVal},
		},
		{
			suffix:     routing.DefaultTopicSuffix,
			partitions: int32(cfg.DefaultPartitions), //nolint:gosec // G115: validated >= 1 at startup
			cfg:        map[string]*string{kafkautil.RetentionMsConfigKey: &defaultRetentionVal},
		},
	}

	tenantRepo := repository.NewTenantRepository(pool)
	statusActive := provisioning.StatusActive
	opts := provisioning.ListOptions{
		Limit:  pageSize,
		Offset: 0,
		Status: &statusActive,
	}

	var totalSucceeded, totalFailed int

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
			ok := provisionTenantTopics(ctx, admin, namespace, tenant, specs, rf, *recreate, *dryRun, log)
			if ok {
				totalSucceeded++
			} else {
				totalFailed++
			}
		}

		opts.Offset += len(tenants)
		if len(tenants) < pageSize {
			break
		}
	}

	log.Info().
		Int("succeeded", totalSucceeded).
		Int("failed", totalFailed).
		Msg("migrate-dlq complete")

	if totalFailed > 0 {
		return 2
	}
	return 0
}

// topicMigSpec defines the creation parameters for a single infrastructure topic.
type topicMigSpec struct {
	suffix     string
	partitions int32
	cfg        map[string]*string
}

// provisionTenantTopics creates the infrastructure topics for a single tenant.
// Returns true if all topics were provisioned successfully (or dry-run), false on any failure.
// The admin parameter is an interface so tests can inject a fake without a live broker.
func provisionTenantTopics(
	ctx context.Context,
	admin topicAdmin,
	namespace string,
	tenant *provisioning.Tenant,
	specs []topicMigSpec,
	rf int16,
	recreate bool,
	dryRun bool,
	log zerolog.Logger,
) bool {
	allOK := true

	for _, spec := range specs {
		topicName := kafkautil.BuildTopicName(namespace, tenant.Slug, spec.suffix)
		tlog := log.With().Str("tenant_slug", tenant.Slug).Str("topic", topicName).Logger()

		if dryRun {
			if recreate {
				tlog.Info().Msg("[dry-run] would delete and recreate topic")
			} else {
				tlog.Info().Msg("[dry-run] would create topic")
			}
			continue
		}

		if recreate {
			deleteResp, err := admin.DeleteTopics(ctx, topicName)
			if err != nil {
				tlog.Error().Err(err).Msg("Failed to delete topic for recreation")
				allOK = false
				continue
			}
			if deleteErr := deleteResp[topicName].Err; deleteErr != nil {
				tlog.Error().Err(deleteErr).Msg("Failed to delete topic for recreation")
				allOK = false
				continue
			}
		}

		resp, err := admin.CreateTopics(ctx, spec.partitions, rf, spec.cfg, topicName)
		if err != nil {
			tlog.Error().Err(err).Msg("Failed to create topic")
			allOK = false
			continue
		}

		if topicErr := resp[topicName].Err; topicErr != nil {
			if errors.Is(topicErr, kerr.TopicAlreadyExists) {
				tlog.Info().Msg("topic already exists (skipped)")
				continue
			}
			// If we just deleted the topic and create failed, it is now absent.
			if recreate {
				tlog.Error().Err(topicErr).Str("state", "topic_absent").Msg("topic deleted but recreation failed — re-run to restore")
			} else {
				tlog.Error().Err(topicErr).Msg("Failed to create topic")
			}
			allOK = false
			continue
		}

		tlog.Info().Msg("topic ready")
	}

	return allOK
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
