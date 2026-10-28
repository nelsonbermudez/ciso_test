// Command ingest: consumes events from Redpanda and indexes them in OpenSearch.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"aisoc/internal/broker"
	"aisoc/internal/config"
	"aisoc/internal/events"
	"aisoc/internal/store"
)

func main() {
	brokers := config.List("REDPANDA_BROKERS")
	topics := config.List("REDPANDA_TOPIC")
	if len(topics) == 0 {
		topics = config.List("REDPANDA_TOPICS") // backward compatibility
	}
	if len(topics) == 0 {
		topics = []string{"soc.events"}
	}
	group := config.Get("CONSUMER_GROUP", "aisoc-ingest")
	osURLs := config.List("OPENSEARCH_URLS")
	index := config.Get("OPENSEARCH_INDEX", "soc-events")

	consumer, err := broker.NewConsumer(brokers, topics, group)
	if err != nil {
		log.Fatalf("consumer: %v", err)
	}
	defer consumer.Close()

	osClient, err := store.NewClient(osURLs)
	if err != nil {
		log.Fatalf("opensearch: %v", err)
	}

	ctx := context.Background()
	if err := store.WaitReady(ctx, osClient, 90*time.Second); err != nil {
		log.Fatalf("opensearch unavailable: %v", err)
	}

	log.Printf("ingest: consuming %v -> index %q", topics, index)
	for {
		fetches := consumer.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return
		}
		for _, e := range fetches.Errors() {
			log.Printf("fetch error: %v", e.Err)
		}
		iter := fetches.RecordIter()
		for !iter.Done() {
			rec := iter.Next()
			var ev events.Event
			if err := json.Unmarshal(rec.Value, &ev); err != nil {
				log.Printf("invalid event (skip): %v", err)
				continue
			}
			docID := fmt.Sprintf("%s-%s", ev.RequestID, ev.Type)
			if err := store.Index(ctx, osClient, index, docID, rec.Value); err != nil {
				log.Printf("index %s: %v", docID, err)
				continue
			}
			log.Printf("indexed %s (%s)", docID, ev.Type)
		}
	}
}
