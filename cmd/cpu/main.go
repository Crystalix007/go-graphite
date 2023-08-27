package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/Crystalix007/go-graphite/graphite"
	cpustats "github.com/mackerelio/go-osstat/cpu"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func main() {
	cmd := &cobra.Command{
		Use:   "cpu",
		Short: "cpu",
		Long:  "cpu",
		RunE:  cpu,
	}

	cmd.Flags().String("addr", "", "Graphite server address")
	cmd.Flags().String("prefix", "", "Graphite metric prefix")
	cmd.Flags().String("tags", "", "Graphite metric tags")
	cmd.Flags().Bool("tls", false, "Use TLS when connecting to Graphite server")
	cmd.Flags().Uint("interval", 1, "Interval between CPU usage reports in seconds")

	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func cpu(cmd *cobra.Command, args []string) error {
	addr, err := cmd.Flags().GetString("addr")
	if err != nil {
		return fmt.Errorf("cmd/cpu: failed to get address string: %w", err)
	}

	prefixString, err := cmd.Flags().GetString("prefix")
	if err != nil {
		return fmt.Errorf("cmd/cpu: failed to get prefix string: %w", err)
	}

	tagsString, err := cmd.Flags().GetString("tags")
	if err != nil {
		return fmt.Errorf("cmd/cpu: failed to get tags string: %w", err)
	}

	interval, err := cmd.Flags().GetUint("interval")
	if err != nil {
		return fmt.Errorf("cmd/cpu: failed to get interval: %w", err)
	}

	tlsEnabled, err := cmd.Flags().GetBool("tls")
	if err != nil {
		return fmt.Errorf("cmd/cpu: failed to get TLS flag: %w", err)
	}

	var conn net.Conn

	if tlsEnabled {
		conn, err = tls.Dial("tcp", addr, nil)
		if err != nil {
			return fmt.Errorf("cmd/cpu: failed to dial TLS connection: %w", err)
		}
	} else {
		conn, err = net.Dial("tcp", addr)
		if err != nil {
			return fmt.Errorf("cmd/cpu: failed to dial TCP connection: %w", err)
		}
	}
	defer conn.Close()

	client, err := graphite.NewClient(
		context.Background(),
		graphite.WithConnection(conn),
	)
	if err != nil {
		return fmt.Errorf("cmd/cpu: failed to create Graphite client: %w", err)
	}

	prefix := strings.Split(prefixString, ".")
	tagsMap := make(map[string]string)

	if tagsString != "" {
		tags := strings.Split(tagsString, ",")
		for _, tag := range tags {
			tagProperties := strings.SplitN(tag, "=", 2)

			if len(tagProperties) != 2 {
				return fmt.Errorf("cmd/cpu: invalid tag: '%s', expected 'name=value'", tag)
			}

			tagsMap[tagProperties[0]] = tagProperties[1]
		}
	}

	tagMetadata := graphite.MetricMetadata{
		Name: prefix,
		Tags: tagsMap,
	}

	if err := reportCPUUsage(
		context.Background(),
		client,
		tagMetadata,
		time.Duration(interval)*time.Second,
	); err != nil {
		return fmt.Errorf("cmd/cpu: failed to report CPU usage: %w", err)
	}

	return nil
}

func reportCPUUsage(ctx context.Context,
	client graphite.Client,
	metricMetadata graphite.MetricMetadata,
	interval time.Duration,
) error {
	var errg errgroup.Group

	errg.Go(func() error {
		return client.Submit(ctx)
	})

	errg.Go(func() error {
		previousStats, err := cpustats.Get()
		if err != nil {
			return fmt.Errorf(
				"cmd/cpu: failed to get CPU stats: %w",
				err,
			)
		}

		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf(
					"cmd/cpu: context cancelled while reporting CPU usage: %w",
					ctx.Err(),
				)
			case <-time.After(interval):
			}

			timestamp := time.Now()

			cpuStats, err := cpustats.Get()
			if err != nil {
				return fmt.Errorf(
					"cmd/cpu: failed to get CPU stats: %w",
					err,
				)
			}

			incrementalCPUStats := map[string]int{
				"idle":   int(cpuStats.Idle - previousStats.Idle),
				"nice":   int(cpuStats.Nice - previousStats.Nice),
				"system": int(cpuStats.System - previousStats.System),
				"total":  int(cpuStats.Total - previousStats.Total),
				"user":   int(cpuStats.User - previousStats.User),
			}

			for name, value := range incrementalCPUStats {
				metric := metricMetadata.SubMetric(name, metricMetadata.Tags)

				if err := client.SendMetric(
					ctx,
					*metric,
					fmt.Sprint(value),
					timestamp,
				); err != nil {
					return fmt.Errorf(
						"cmd/cpu: failed to queue CPU metric: %w",
						err,
					)
				}
			}
		}
	})

	return errg.Wait()
}
