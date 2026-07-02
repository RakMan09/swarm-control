// Package config centralizes environment-driven configuration for all
// SwarmControl backend services so they can be wired identically in local dev,
// tests, and docker-compose.
package config

import (
	"os"
	"strconv"
	"time"
)

// Common holds settings shared by every backend service.
type Common struct {
	MQTTBroker   string // e.g. tcp://localhost:1883
	MQTTClientID string
	RedisAddr    string // host:port
	PostgresDSN  string // postgres connection string
}

// LoadCommon reads the shared settings from the environment.
func LoadCommon(defaultClientID string) Common {
	return Common{
		MQTTBroker:   env("MQTT_BROKER", "tcp://localhost:1883"),
		MQTTClientID: env("MQTT_CLIENT_ID", defaultClientID),
		RedisAddr:    env("REDIS_ADDR", "localhost:6379"),
		PostgresDSN:  env("POSTGRES_DSN", "postgres://swarm:swarm@localhost:5432/swarm?sslmode=disable"),
	}
}

// Ingestion configures the ingestion service's batching + backpressure.
type Ingestion struct {
	Common
	QueueSize    int           // bounded channel capacity (backpressure boundary)
	BatchSize    int           // max rows per DB insert
	BatchTimeout time.Duration // flush partial batch after this long
	DropOnFull   bool          // true => shed load, false => block (delay)
	MetricsAddr  string        // :9101
	TwinWorkers  int           // concurrent Redis twin writers
}

// LoadIngestion builds the ingestion configuration.
func LoadIngestion() Ingestion {
	return Ingestion{
		Common:       LoadCommon("swarm-ingestion"),
		QueueSize:    envInt("INGEST_QUEUE_SIZE", 50000),
		BatchSize:    envInt("INGEST_BATCH_SIZE", 500),
		BatchTimeout: envDuration("INGEST_BATCH_TIMEOUT", 200*time.Millisecond),
		DropOnFull:   envBool("INGEST_DROP_ON_FULL", true),
		MetricsAddr:  env("INGEST_METRICS_ADDR", ":9101"),
		TwinWorkers:  envInt("INGEST_TWIN_WORKERS", 8),
	}
}

// Alerts configures the streaming alert engine.
type Alerts struct {
	Common
	LowBatteryPct   float64       // fire when battery below this
	LostCommsAfter  time.Duration // no telemetry for this long => lost-comms
	SweepInterval   time.Duration // how often the lost-comms sweep runs
	AnomalyK        float64       // temp > mean + k*stddev
	AnomalyWindow   int           // rolling window sample count
	GeofencePolygon string        // JSON [[lat,lon],...]; empty => default box
	MetricsAddr     string        // :9102
}

// LoadAlerts builds the alert-engine configuration.
func LoadAlerts() Alerts {
	return Alerts{
		Common:          LoadCommon("swarm-alerts"),
		LowBatteryPct:   envFloat("ALERT_LOW_BATTERY_PCT", 15),
		LostCommsAfter:  envDuration("ALERT_LOST_COMMS_AFTER", 10*time.Second),
		SweepInterval:   envDuration("ALERT_SWEEP_INTERVAL", 2*time.Second),
		AnomalyK:        envFloat("ALERT_ANOMALY_K", 3.0),
		AnomalyWindow:   envInt("ALERT_ANOMALY_WINDOW", 30),
		GeofencePolygon: env("ALERT_GEOFENCE_POLYGON", ""),
		MetricsAddr:     env("ALERT_METRICS_ADDR", ":9102"),
	}
}

// Command configures the command/ack service.
type Command struct {
	Common
	HTTPAddr      string        // :8082
	AckTimeout    time.Duration // mark undelivered after this long
	SweepInterval time.Duration // how often undelivered sweep runs
}

// LoadCommand builds the command-service configuration.
func LoadCommand() Command {
	return Command{
		Common:        LoadCommon("swarm-command"),
		HTTPAddr:      env("COMMAND_HTTP_ADDR", ":8082"),
		AckTimeout:    envDuration("COMMAND_ACK_TIMEOUT", 15*time.Second),
		SweepInterval: envDuration("COMMAND_SWEEP_INTERVAL", 2*time.Second),
	}
}

// API configures the read/websocket API service.
type API struct {
	Common
	HTTPAddr       string // :8081
	CommandBaseURL string // base URL of the command service for POST /commands
}

// LoadAPI builds the API-service configuration.
func LoadAPI() API {
	return API{
		Common:         LoadCommon("swarm-api"),
		HTTPAddr:       env("API_HTTP_ADDR", ":8081"),
		CommandBaseURL: env("COMMAND_BASE_URL", "http://localhost:8082"),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
