"""Simulated vehicle: a deterministic, seedable motion + battery model with
optional fault injection. Pure standard library so it is trivial to unit-test.

Coordinates are WGS84 lat/lon; motion is integrated in metres and converted
back to degrees each step.
"""
from __future__ import annotations

import math
import random
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Optional

# metres per degree of latitude (good enough for a local sim area)
_M_PER_DEG_LAT = 111_320.0

# Fault modes a vehicle can be assigned at spawn time.
FAULT_NONE = "none"
FAULT_STRAY = "stray"      # drives out of the geofence
FAULT_DRAIN = "drain"      # battery drains abnormally fast
FAULT_SILENT = "silent"    # stops reporting after a while (lost-comms)
FAULT_HOT = "hot"          # emits temperature spikes (anomaly)

ALL_FAULTS = (FAULT_STRAY, FAULT_DRAIN, FAULT_SILENT, FAULT_HOT)


@dataclass
class Vehicle:
    vehicle_id: str
    lat: float
    lon: float
    heading_deg: float
    speed: float                     # m/s
    battery_pct: float = 100.0
    temp: float = 25.0
    status: str = "active"
    fault: str = FAULT_NONE
    seed: int = 0

    rng: random.Random = field(default_factory=random.Random, repr=False)
    _age: float = 0.0                # seconds since spawn
    _silent_after: float = 0.0       # for FAULT_SILENT
    _base_temp: float = 25.0

    def __post_init__(self) -> None:
        self.rng = random.Random(self.seed)
        self._base_temp = self.temp
        # Silent vehicles go dark somewhere between 15 and 45 seconds in.
        self._silent_after = 15.0 + self.rng.random() * 30.0

    @classmethod
    def spawn(
        cls,
        index: int,
        base_seed: int,
        origin_lat: float,
        origin_lon: float,
        area_deg: float,
        fault: str = FAULT_NONE,
    ) -> "Vehicle":
        """Create a vehicle deterministically from (index, base_seed)."""
        seed = base_seed * 1_000_003 + index
        rng = random.Random(seed)
        lat = origin_lat + (rng.random() - 0.5) * area_deg
        lon = origin_lon + (rng.random() - 0.5) * area_deg
        return cls(
            vehicle_id=f"veh-{index:05d}",
            lat=lat,
            lon=lon,
            heading_deg=rng.random() * 360.0,
            speed=5.0 + rng.random() * 10.0,
            battery_pct=80.0 + rng.random() * 20.0,
            temp=22.0 + rng.random() * 6.0,
            fault=fault,
            seed=seed,
        )

    def is_silent(self) -> bool:
        return self.fault == FAULT_SILENT and self._age >= self._silent_after

    def step(self, dt: float) -> None:
        """Advance the simulation by dt seconds."""
        self._age += dt

        # Heading: gentle random walk, except stray vehicles drive due-east out
        # of the fence, and returning vehicles hold their (already-set) heading.
        if self.fault == FAULT_STRAY:
            self.heading_deg = 90.0
            self.speed = max(self.speed, 12.0)
        elif self.status != "returning":
            self.heading_deg = (self.heading_deg + (self.rng.random() - 0.5) * 20.0) % 360.0

        # Integrate position.
        dist = self.speed * dt
        rad = math.radians(self.heading_deg)
        dlat = math.cos(rad) * dist / _M_PER_DEG_LAT
        dlon = math.sin(rad) * dist / (_M_PER_DEG_LAT * math.cos(math.radians(self.lat)) or 1e-9)
        self.lat += dlat
        self.lon += dlon

        # Battery drain (per second), amplified for the drain fault.
        drain = 0.05 + self.speed * 0.002
        if self.fault == FAULT_DRAIN:
            drain *= 8.0
        self.battery_pct = max(0.0, self.battery_pct - drain * dt)

        # Temperature: baseline + load + occasional spike for hot vehicles.
        self.temp = self._base_temp + self.speed * 0.1 + (self.rng.random() - 0.5) * 0.5
        if self.fault == FAULT_HOT and self.rng.random() < 0.05:
            self.temp += 30.0 + self.rng.random() * 15.0

        # Derive status.
        if self.battery_pct <= 15.0:
            self.status = "low_batt"
        elif self.status != "returning":
            self.status = "active"

    def telemetry(self, ts: Optional[datetime] = None) -> dict:
        """Build a telemetry message matching the platform wire schema."""
        if ts is None:
            ts = datetime.now(timezone.utc)
        return {
            "vehicleId": self.vehicle_id,
            "ts": ts.isoformat(),
            "lat": round(self.lat, 6),
            "lon": round(self.lon, 6),
            "headingDeg": round(self.heading_deg % 360.0, 2),
            "speed": round(self.speed, 3),
            "batteryPct": round(self.battery_pct, 2),
            "temp": round(self.temp, 2),
            "status": self.status,
        }

    def apply_command(self, cmd_type: str, payload: Optional[dict], base_lat: float, base_lon: float) -> str:
        """Apply a command and return the ack state ('applied')."""
        payload = payload or {}
        t = (cmd_type or "").lower()
        if t in ("rtb", "return_to_base", "return-to-base"):
            self.status = "returning"
            self.fault = FAULT_NONE  # returning cancels stray behaviour
            self.heading_deg = self._bearing_to(base_lat, base_lon)
        elif t == "reroute":
            if "headingDeg" in payload:
                self.heading_deg = float(payload["headingDeg"]) % 360.0
            elif "lat" in payload and "lon" in payload:
                self.heading_deg = self._bearing_to(float(payload["lat"]), float(payload["lon"]))
            self.status = "active"
        elif t in ("set_geofence", "set-geofence"):
            # Sim just acknowledges; the alert engine owns fence evaluation.
            pass
        return "applied"

    def _bearing_to(self, lat: float, lon: float) -> float:
        dlat = lat - self.lat
        dlon = lon - self.lon
        return math.degrees(math.atan2(dlon, dlat)) % 360.0
