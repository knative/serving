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

import argparse
import gzip
import importlib.util
import json
from pathlib import Path
import tempfile
import time
import unittest
import urllib.error
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("mesh_observability", Path(__file__).with_name("mesh-observability.py"))
observer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(observer)


class ObservabilityTests(unittest.TestCase):
    def setUp(self):
        observer._next_log_request_at = 0
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.output = Path(self.directory.name)
        self.args = argparse.Namespace(output=self.output, namespace="serving", project="test-project", provider="gke", interval=30)
        self.metadata = {"project": "test-project", "cluster": "test-cluster", "location": "us-east1",
                         "started_at": "2026-11-07T00:00:00Z", "ended_at": "2026-11-07T01:00:00Z", "test_namespace": "serving-tests"}

    def test_telemetry_verification(self):
        self.assertFalse(observer.telemetry_status({})["system_and_workload_logging_enabled"])
        self.assertFalse(observer.telemetry_status({"loggingConfig": None, "monitoringConfig": None})["system_monitoring_enabled"])
        config = {"loggingConfig": {"componentConfig": {"enableComponents": ["SYSTEM_COMPONENTS", "WORKLOADS"]}},
                  "monitoringConfig": {"componentConfig": {"enableComponents": ["SYSTEM_COMPONENTS"]}}}
        self.assertTrue(observer.telemetry_status(config)["system_and_workload_logging_enabled"])
        self.assertTrue(observer.telemetry_status(config)["system_monitoring_enabled"])

    def test_discover_does_not_persist_cluster_credentials(self):
        def capture(cmd, timeout=10):
            if cmd[0] == "git":
                return 0, b"test-sha\n", b""
            if cmd[0] == "go":
                return 0, b"go version test\n", b""
            if cmd[:3] == ["kubectl", "config", "current-context"]:
                return 0, b"gke_test-project_us-east1_test-cluster\n", b""
            if cmd[0] == "kubectl":
                return 0, b'{"metadata":{"uid":"cluster-uid"}}', b""
            config = {"name": "test-cluster", "masterAuth": {"clientKey": "PRIVATE-KEY"}}
            return 0, json.dumps(config).encode(), b""
        with patch.object(observer, "command", side_effect=capture):
            metadata = observer.discover(self.args)
        self.assertEqual(metadata["cluster"], "test-cluster")
        self.assertEqual(metadata["kube_system_namespace_uid"], "cluster-uid")
        for file in self.output.iterdir():
            self.assertNotIn("PRIVATE-KEY", file.read_text())
            self.assertNotIn("masterAuth", file.read_text())

    def test_refuse_wrong_project(self):
        with patch.object(observer, "command", side_effect=[
            (0, b"sha", b""), (0, b"go version test", b""), (0, b"gke_other-project_us-east1_test-cluster", b""),
            (0, b'{"metadata":{"uid":"uid"}}', b""),
        ]):
            with self.assertRaisesRegex(ValueError, "differs"):
                observer.discover(self.args)

    def test_cloud_logs_are_cluster_time_namespace_scoped(self):
        query = observer.cloud_filter(self.metadata, "serving")
        for expected in ['cluster_name="test-cluster"', 'location="us-east1"', 'namespace_name="serving"',
                         self.metadata["started_at"], self.metadata["ended_at"], 'NOT logName:"cloudaudit.googleapis.com"']:
            self.assertIn(expected, query)

    def test_logs_pagination_and_no_token_artifacts(self):
        with patch.object(observer, "api", side_effect=[
            {"entries": [{"textPayload": "first"}], "nextPageToken": "next"},
            {"entries": [{"textPayload": "second"}]},
        ]) as api:
            result = observer.export_logs(self.metadata, "PRIVATE-TOKEN", self.output, "logs", "scoped-filter", time.monotonic() + 10)
        self.assertEqual(api.call_count, 2)
        self.assertEqual(api.call_args_list[1].args[2]["pageToken"], "next")
        self.assertEqual(result["entries"], 2)
        self.assertTrue(result["query_completed"])
        with gzip.open(self.output / "logs.jsonl.gz", "rt") as file:
            data = file.read()
        self.assertNotIn("PRIVATE-TOKEN", data)
        self.assertEqual(len(data.splitlines()), 2)

    def test_logs_limit_is_explicitly_incomplete(self):
        with patch.object(observer, "api", return_value={"entries": [{"textPayload": "first"}], "nextPageToken": "next"}):
            result = observer.export_logs(self.metadata, "token", self.output, "logs", "filter", time.monotonic() + 10, limit=1)
        self.assertFalse(result["query_completed"])
        self.assertEqual(result["entries"], 1)

    def test_logging_429_backs_off_and_retries(self):
        now = [0.0]
        error = urllib.error.HTTPError("https://logging.googleapis.com", 429, "rate limited", {"Retry-After": "6"}, None)
        with patch.object(observer.time, "monotonic", side_effect=lambda: now[0]), \
                patch.object(observer.time, "sleep", side_effect=lambda delay: now.__setitem__(0, now[0] + delay)), \
                patch.object(observer, "api", side_effect=[error, {"entries": [{"textPayload": "entry"}]}]) as api:
            result = observer.export_logs(self.metadata, "token", self.output, "logs", "filter", deadline=10)
        self.assertEqual(result["rate_limited_requests"], 1)
        self.assertEqual(result["entries"], 1)
        self.assertTrue(result["query_completed"])
        self.assertGreaterEqual(now[0], 6)
        self.assertEqual(api.call_args.args[2]["orderBy"], "timestamp desc")

    def test_logging_429_respects_export_deadline(self):
        now = [0.0]
        error = urllib.error.HTTPError("https://logging.googleapis.com", 429, "rate limited", {}, None)
        with patch.object(observer.time, "monotonic", side_effect=lambda: now[0]), \
                patch.object(observer.time, "sleep", side_effect=lambda delay: now.__setitem__(0, now[0] + delay)), \
                patch.object(observer, "api", side_effect=error) as api:
            result = observer.export_logs(self.metadata, "token", self.output, "logs", "filter", deadline=3)
        self.assertEqual(api.call_count, 1)
        self.assertFalse(result["query_completed"])
        self.assertEqual(result["rate_limited_requests"], 1)

    def test_metrics_are_paginated(self):
        responses = [{"timeSeries": [{"metric": {"type": "cpu"}}], "nextPageToken": "next"},
                     {"timeSeries": [{"metric": {"type": "cpu"}}]}] + [{}] * 6
        with patch.object(observer, "api", side_effect=responses) as api:
            result = observer.export_metrics(self.metadata, "token", self.output, time.monotonic() + 10)
        metric = "kubernetes.io/node/cpu/allocatable_utilization"
        self.assertEqual(result[metric]["series"], 2)
        self.assertTrue(result[metric]["query_completed"])
        self.assertIn("pageToken=next", api.call_args_list[1].args[0])
        self.assertEqual(api.call_count, 8)

    def test_unavailable_metric_does_not_hide_other_metrics(self):
        with patch.object(observer, "api", side_effect=[RuntimeError("metric unavailable")] + [{}] * 6):
            result = observer.export_metrics(self.metadata, "token", self.output, time.monotonic() + 10)
        self.assertFalse(result["kubernetes.io/node/cpu/allocatable_utilization"]["query_completed"])
        self.assertTrue(result["kubernetes.io/container/restart_count"]["query_completed"])

    def test_stop_prevents_further_kubernetes_queries(self):
        with patch.object(observer, "command") as capture:
            observer.sample(self.args, lambda: True)
        capture.assert_not_called()

    def test_kind_export_never_requests_gcp_credentials(self):
        self.args.provider = "kind"
        observer.write_json(self.output / "run.json", self.metadata)
        with patch.object(observer, "command") as capture:
            observer.export(self.args)
        capture.assert_not_called()
        self.assertEqual(json.loads((self.output / "export-status.json").read_text())["status"], "not applicable to kind")


if __name__ == "__main__":
    unittest.main()
