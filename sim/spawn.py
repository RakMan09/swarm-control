"""Spawn a swarm of simulated vehicles that publish telemetry over MQTT and
respond to commands with acks.

Usage:
    python sim/spawn.py --vehicles 2000 --rate 1 --broker localhost:1883
"""
from __future__ import annotations

import argparse
import json
import os
import signal
import sys
import threading
import time
from datetime import datetime, timezone

import paho.mqtt.client as mqtt

from vehicle import ALL_FAULTS, FAULT_NONE, Vehicle


def parse_args(argv=None) -> argparse.Namespace:
    p = argparse.ArgumentParser(description="SwarmControl vehicle fleet simulator")
    p.add_argument("--vehicles", type=int, default=int(os.getenv("SIM_VEHICLES", "50")))
    p.add_argument("--rate", type=float, default=float(os.getenv("SIM_RATE_HZ", "1")),
                   help="telemetry publishes per vehicle per second")
    p.add_argument("--broker", default=os.getenv("MQTT_BROKER_HOSTPORT", "localhost:1883"))
    p.add_argument("--qos", type=int, default=int(os.getenv("SIM_QOS", "0")), choices=[0, 1])
    p.add_argument("--seed", type=int, default=int(os.getenv("SIM_SEED", "42")))
    p.add_argument("--fault-rate", type=float, default=float(os.getenv("SIM_FAULT_RATE", "0.05")),
                   help="fraction of vehicles assigned a random fault")
    p.add_argument("--duration", type=float, default=float(os.getenv("SIM_DURATION", "0")),
                   help="run seconds then exit (0 = forever)")
    p.add_argument("--origin-lat", type=float, default=float(os.getenv("SIM_ORIGIN_LAT", "37.5")))
    p.add_argument("--origin-lon", type=float, default=float(os.getenv("SIM_ORIGIN_LON", "-122.05")))
    p.add_argument("--area", type=float, default=float(os.getenv("SIM_AREA_DEG", "0.08")),
                   help="initial spread of vehicles in degrees")
    return p.parse_args(argv)


def build_fleet(args) -> dict:
    fleet = {}
    for i in range(args.vehicles):
        fault = FAULT_NONE
        # Deterministic fault assignment so runs are reproducible.
        if args.fault_rate > 0 and (i * 2_654_435_761) % 1000 < args.fault_rate * 1000:
            fault = ALL_FAULTS[i % len(ALL_FAULTS)]
        v = Vehicle.spawn(i, args.seed, args.origin_lat, args.origin_lon, args.area, fault)
        fleet[v.vehicle_id] = v
    return fleet


def main(argv=None) -> int:
    args = parse_args(argv)
    host, _, port = args.broker.partition(":")
    port = int(port or "1883")

    fleet = build_fleet(args)
    print(f"sim: spawning {len(fleet)} vehicles @ {args.rate}Hz -> {host}:{port} "
          f"(faults={sum(1 for v in fleet.values() if v.fault != FAULT_NONE)})", flush=True)

    stop = threading.Event()
    lock = threading.Lock()

    client = mqtt.Client(client_id=f"sim-{os.getpid()}", clean_session=True)

    def on_connect(c, _u, _f, rc):
        c.subscribe("fleet/+/cmd", qos=1)
        print(f"sim: connected (rc={rc}), subscribed to fleet/+/cmd", flush=True)

    def on_message(_c, _u, msg):
        # topic: fleet/{vehicleId}/cmd ; payload: {id, type, payload}
        parts = msg.topic.split("/")
        if len(parts) != 3:
            return
        vid = parts[1]
        try:
            data = json.loads(msg.payload.decode())
        except Exception:
            return
        with lock:
            v = fleet.get(vid)
            if v is None:
                return
            state = v.apply_command(data.get("type", ""), data.get("payload"), args.origin_lat, args.origin_lon)
        ack = {"id": data.get("id"), "vehicleId": vid, "state": state}
        client.publish(f"fleet/{vid}/ack", json.dumps(ack), qos=1)

    client.on_connect = on_connect
    client.on_message = on_message
    client.connect(host, port, keepalive=30)
    client.loop_start()

    def handle_sig(*_):
        stop.set()
    signal.signal(signal.SIGINT, handle_sig)
    signal.signal(signal.SIGTERM, handle_sig)

    dt = 1.0 / args.rate if args.rate > 0 else 1.0
    start = time.time()
    published = 0
    last_report = start

    while not stop.is_set():
        tick_start = time.time()
        now = datetime.now(timezone.utc)
        with lock:
            for v in fleet.values():
                v.step(dt)
                if v.is_silent():
                    continue
                msg = json.dumps(v.telemetry(now))
                client.publish(f"fleet/{v.vehicle_id}/telemetry", msg, qos=args.qos)
                published += 1

        if tick_start - last_report >= 5.0:
            rate = published / (tick_start - start)
            print(f"sim: {published} msgs published (~{rate:.0f}/s avg)", flush=True)
            last_report = tick_start

        if args.duration and (tick_start - start) >= args.duration:
            break

        elapsed = time.time() - tick_start
        time.sleep(max(0.0, dt - elapsed))

    client.loop_stop()
    client.disconnect()
    print(f"sim: stopped after {published} messages", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
