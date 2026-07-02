// Command ingestion subscribes to fleet telemetry over MQTT, validates each
// message, writes it to TimescaleDB via a batched/backpressured writer, updates
// the Redis digital twin, and fans the stream out to the alert engine.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rakman09/swarm-control/internal/bus"
	"github.com/rakman09/swarm-control/internal/config"
	"github.com/rakman09/swarm-control/internal/metrics"
	"github.com/rakman09/swarm-control/internal/mqttutil"
	"github.com/rakman09/swarm-control/internal/store"
	"github.com/rakman09/swarm-control/internal/telemetry"
	"github.com/rakman09/swarm-control/internal/twin"
)

const telemetryTopic = "fleet/+/telemetry"

func main() {
	cfg := config.LoadIngestion()
	ctx, cancel := signalContext()
	defer cancel()

	reg := metrics.NewRegistry()
	reg.Serve(cfg.MetricsAddr)
	log.Printf("ingestion: metrics on %s", cfg.MetricsAddr)

	writer, err := store.New(ctx, cfg, reg)
	if err != nil {
		log.Fatalf("ingestion: store: %v", err)
	}
	defer writer.Close()
	go writer.Run(ctx)

	twins := twin.New(cfg.RedisAddr)
	if err := twins.Ping(ctx); err != nil {
		log.Fatalf("ingestion: redis: %v", err)
	}
	b := bus.NewWithClient(twins.Client())

	// Secondary fan-out (twin + alert stream) is best-effort: drop under load so
	// the primary DB write path is never blocked by Redis latency.
	invalid := reg.Counter("ingest_invalid_total")
	fanoutDropped := reg.Counter("ingest_fanout_dropped_total")
	fanout := make(chan telemetry.Message, cfg.QueueSize)
	for i := 0; i < cfg.TwinWorkers; i++ {
		go func() {
			for m := range fanout {
				wc, cancelW := context.WithTimeout(ctx, 2*time.Second)
				_ = twins.Update(wc, m)
				_ = b.PublishTelemetry(wc, m)
				cancelW()
			}
		}()
	}

	onMessage := func(_ mqtt.Client, msg mqtt.Message) {
		m, err := telemetry.Parse(msg.Payload())
		if err != nil {
			invalid.Inc()
			return
		}
		writer.Enqueue(m)
		select {
		case fanout <- m:
		default:
			fanoutDropped.Inc()
		}
	}

	onConnect := func(c mqtt.Client) {
		if tok := c.Subscribe(telemetryTopic, 1, onMessage); tok.Wait() && tok.Error() != nil {
			log.Printf("ingestion: subscribe error: %v", tok.Error())
		} else {
			log.Printf("ingestion: subscribed to %s", telemetryTopic)
		}
	}

	client, err := mqttutil.Connect(cfg.MQTTBroker, cfg.MQTTClientID, true, onConnect, 60*time.Second)
	if err != nil {
		log.Fatalf("ingestion: mqtt: %v", err)
	}
	defer client.Disconnect(500)

	log.Printf("ingestion: running (queue=%d batch=%d dropOnFull=%v)", cfg.QueueSize, cfg.BatchSize, cfg.DropOnFull)
	<-ctx.Done()
	close(fanout)
	log.Printf("ingestion: shutting down")
	time.Sleep(300 * time.Millisecond) // let final batch flush
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
