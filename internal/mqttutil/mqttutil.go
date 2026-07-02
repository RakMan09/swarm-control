// Package mqttutil centralizes creation of Eclipse Paho MQTT clients with the
// connection options SwarmControl services rely on (auto-reconnect, clean
// session tuning, resilient connect retry).
package mqttutil

import (
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Connect builds and connects an MQTT client, retrying until it succeeds or
// maxWait elapses. onConnect is registered so subscriptions survive reconnects.
func Connect(broker, clientID string, cleanSession bool, onConnect mqtt.OnConnectHandler, maxWait time.Duration) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetCleanSession(cleanSession).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetMaxReconnectInterval(10 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOrderMatters(false)
	if onConnect != nil {
		opts.SetOnConnectHandler(onConnect)
	}

	client := mqtt.NewClient(opts)
	deadline := time.Now().Add(maxWait)
	for {
		tok := client.Connect()
		if tok.WaitTimeout(5*time.Second) && tok.Error() == nil {
			return client, nil
		}
		if time.Now().After(deadline) {
			if err := tok.Error(); err != nil {
				return nil, fmt.Errorf("mqtt connect: %w", err)
			}
			return nil, fmt.Errorf("mqtt connect: timed out connecting to %s", broker)
		}
		time.Sleep(2 * time.Second)
	}
}
