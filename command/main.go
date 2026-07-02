// Command command exposes an HTTP API to send commands to vehicles over MQTT
// (QoS 1) and tracks their lifecycle (sent -> acked -> applied), flagging
// undelivered commands after a timeout. Acks arrive on fleet/+/ack.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rakman09/swarm-control/internal/command"
	"github.com/rakman09/swarm-control/internal/config"
	"github.com/rakman09/swarm-control/internal/mqttutil"
)

const ackTopic = "fleet/+/ack"

type ackMessage struct {
	ID        string `json:"id"`
	VehicleID string `json:"vehicleId"`
	State     string `json:"state"` // acked | applied
}

type createRequest struct {
	VehicleID string          `json:"vehicleId"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func main() {
	cfg := config.LoadCommand()
	ctx, cancel := signalContext()
	defer cancel()

	persist := newPersister(ctx, cfg.PostgresDSN)
	tracker := command.NewTracker(persist)

	client, err := mqttutil.Connect(cfg.MQTTBroker, cfg.MQTTClientID, false, func(c mqtt.Client) {
		if tok := c.Subscribe(ackTopic, 1, func(_ mqtt.Client, msg mqtt.Message) {
			var ack ackMessage
			if err := json.Unmarshal(msg.Payload(), &ack); err != nil || ack.ID == "" {
				return
			}
			if _, err := tracker.Ack(ack.ID, ack.State); err != nil {
				log.Printf("command: ack for unknown id %s", ack.ID)
			}
		}); tok.Wait() && tok.Error() != nil {
			log.Printf("command: subscribe ack error: %v", tok.Error())
		} else {
			log.Printf("command: subscribed to %s", ackTopic)
		}
	}, 60*time.Second)
	if err != nil {
		log.Fatalf("command: mqtt: %v", err)
	}
	defer client.Disconnect(500)

	// Undelivered sweep.
	go func() {
		t := time.NewTicker(cfg.SweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, c := range tracker.SweepUndelivered(cfg.AckTimeout) {
					log.Printf("command: %s to %s undelivered after %s", c.ID, c.VehicleID, cfg.AckTimeout)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/commands", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		switch r.Method {
		case http.MethodOptions:
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			writeJSON(w, http.StatusOK, tracker.List(r.URL.Query().Get("vehicleId")))
		case http.MethodPost:
			handleCreate(w, r, client, tracker)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/commands/", func(w http.ResponseWriter, r *http.Request) {
		cors(w)
		id := strings.TrimPrefix(r.URL.Path, "/commands/")
		c, err := tracker.Get(id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	go func() {
		log.Printf("command: HTTP on %s (ackTimeout=%s)", cfg.HTTPAddr, cfg.AckTimeout)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("command: http: %v", err)
		}
	}()

	<-ctx.Done()
	shutCtx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = srv.Shutdown(shutCtx)
	log.Printf("command: shutting down")
}

func handleCreate(w http.ResponseWriter, r *http.Request, client mqtt.Client, tracker *command.Tracker) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VehicleID == "" || req.Type == "" {
		http.Error(w, "vehicleId and type are required", http.StatusBadRequest)
		return
	}
	c := tracker.Create(req.VehicleID, req.Type, req.Payload)

	payload, _ := json.Marshal(map[string]any{
		"id":      c.ID,
		"type":    c.Type,
		"payload": req.Payload,
	})
	topic := "fleet/" + req.VehicleID + "/cmd"
	tok := client.Publish(topic, 1, false, payload)
	tok.WaitTimeout(3 * time.Second)
	if tok.Error() != nil {
		http.Error(w, "publish failed: "+tok.Error().Error(), http.StatusBadGateway)
		return
	}
	sent, _ := tracker.MarkSent(c.ID)
	writeJSON(w, http.StatusAccepted, sent)
}

// newPersister returns a Persister backed by the command_log table, or a no-op
// if Postgres is unavailable (commands still work; only audit is skipped).
func newPersister(ctx context.Context, dsn string) command.Persister {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil || pool.Ping(ctx) != nil {
		log.Printf("command: Postgres unavailable, running without command_log audit")
		return nil
	}
	return func(c command.Command) {
		_, _ = pool.Exec(ctx, `
			INSERT INTO command_log (id, vehicle_id, type, payload, state, created_at, sent_at, acked_at, applied_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (id) DO UPDATE SET
			  state = EXCLUDED.state, sent_at = EXCLUDED.sent_at,
			  acked_at = EXCLUDED.acked_at, applied_at = EXCLUDED.applied_at`,
			c.ID, c.VehicleID, c.Type, nullableJSON(c.Payload), string(c.State),
			c.CreatedAt, c.SentAt, c.AckedAt, c.AppliedAt)
	}
}

func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return []byte(b)
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
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
