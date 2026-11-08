# Copyright 2026 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Bounded, observation-only triggers from unfiltered component logs."""

import datetime
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.parse


class Detector:
    def __init__(self, duration=10, gap=5):
        self.duration, self.gap = duration, gap
        self.failures = {}

    def record(self, pod, row, now):
        message = row.get("message", "")
        revision = row.get("knative.dev/key", "")
        bucket = row.get("bucket", "")
        if message == "Stat forwarding connection established" or message.startswith("Connected to ws://"):
            self.failures.pop((pod, "forwarding", bucket or revision), None)
            return None
        if message == "Autoscaler connection established":
            self.failures.pop((pod, "activator-stats", ""), None)
            return None
        if message == "Using ClusterIP for revision backends":
            self.failures.pop((pod, "backend-probe", revision), None)
            return None

        if message == "Error while processing stat":
            cause, identity = "forwarding", bucket or revision
        elif message == "Failed to establish stat forwarding connection":
            host = urllib.parse.urlparse(row.get("address", "")).hostname or ""
            # Direct Pod-IP failures are expected in strict mesh mode.
            if not host.startswith("autoscaler-bucket-"):
                return None
            cause, identity = "forwarding", bucket or host.split(".")[0]
        elif message.startswith("Autoscaler is not reachable from activator"):
            cause, identity = "activator-stats", ""
        elif message.startswith("Failed to probe clusterIP ") and row.get("ready_backends", 0) > 0:
            cause, identity = "backend-probe", revision
        else:
            return None

        key = (pod, cause, identity)
        previous = self.failures.get(key)
        if not previous or now - previous["last_seen"] > self.gap:
            previous = {"first_seen": now, "count": 0}
        previous.update(last_seen=now, count=previous["count"] + 1)
        self.failures[key] = previous
        if previous["count"] < 3 or now - previous["first_seen"] < self.duration:
            return None
        return {
            "cause": cause, "pod": pod, "revision": revision, "bucket": bucket,
            "duration_seconds": now - previous["first_seen"], "count": previous["count"],
            "last_record": {k: row.get(k) for k in (
                "timestamp", "message", "error", "address", "holder", "retry",
                "queued_stats", "stat_age", "ready_backends", "not_ready_backends",
                "revisions", "stat_message_count", "initiating_revision",
            )},
        }


def recent(row):
    timestamp = row.get("timestamp")
    if timestamp is None:
        return False
    try:
        if isinstance(timestamp, (int, float)):
            then = float(timestamp)
        else:
            then = datetime.datetime.fromisoformat(timestamp.replace("Z", "+00:00")).timestamp()
        return -5 <= time.time() - then <= 60
    except (ValueError, TypeError):
        return False


def monitor(args, stop):
    detector = Detector()
    positions = {}
    captured = {}
    count, last_capture = 0, -120.0
    child, log = None, None
    needles = ("Error while processing stat", "stat forwarding connection", "Stat forwarding connection",
               "Connected to ws://", "Autoscaler connection established", "Autoscaler is not reachable",
               "Failed to probe clusterIP", "Using ClusterIP for revision backends")
    directory = args.logs / args.namespace
    try:
        while not stop():
            if child and child.poll() is not None:
                log.close()
                child, log = None, None
            for file in directory.glob("*.log"):
                if stop():
                    break
                if not file.name.startswith(("autoscaler-", "activator-")) or file.name.startswith("autoscaler-hpa-"):
                    continue
                try:
                    offset, pending = positions.get(file, (0, b""))
                    if file.stat().st_size < offset:
                        offset, pending = 0, b""
                    with file.open("rb") as stream:
                        stream.seek(offset)
                        data = stream.read(256 * 1024)
                        offset = stream.tell()
                    lines = (pending + data).split(b"\n")
                    positions[file] = (offset, lines.pop())
                except OSError:
                    continue
                for line in lines:
                    text = line.decode(errors="replace")
                    if not any(needle in text for needle in needles):
                        continue
                    try:
                        row = json.loads(text[text.index("{"):])
                    except (ValueError, TypeError):
                        continue
                    if not recent(row):
                        continue
                    event = detector.record(file.stem, row, time.monotonic())
                    now = time.monotonic()
                    if not event or child or count >= 5 or now - last_capture < 120:
                        continue
                    identity = (event["pod"], event["cause"], event["bucket"] or event["revision"])
                    if now - captured.get(identity, -300.0) < 300:
                        continue
                    captured[identity] = now
                    count += 1
                    last_capture = now
                    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
                    phase = f"trigger-{event['cause']}-{stamp}-{count}"
                    event.update(namespace=args.namespace, collected_at=stamp, phase=phase)
                    path = args.output / "triggers" / phase
                    path.mkdir(parents=True, exist_ok=True)
                    (path / "event.json").write_text(json.dumps(event, indent=2) + "\n")
                    env = dict(os.environ, SYSTEM_NAMESPACE=args.namespace,
                               ARTIFACTS=os.environ.get("ARTIFACTS", str(args.output.parent)),
                               MESH_DIAGNOSTIC_TARGET_POD=event["pod"])
                    script = Path(__file__).with_name("dump-mesh-diagnostics.sh")
                    log = (path / "collector.log").open("w")
                    child = subprocess.Popen(["timeout", "--kill-after=10s", "90s", "bash", str(script), phase],
                                             env=env, stdout=log, stderr=log)
                    print("Mesh snapshot triggered:", json.dumps(event), flush=True)
            for _ in range(4):
                if stop():
                    break
                time.sleep(0.25)
    finally:
        if child:
            try:
                child.wait(timeout=20)
            except subprocess.TimeoutExpired:
                child.terminate()
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
        if log:
            log.close()
