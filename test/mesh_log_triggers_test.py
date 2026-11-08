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

import datetime
import unittest

from mesh_log_triggers import Detector, recent


class TriggerTests(unittest.TestCase):
    def setUp(self):
        self.detector = Detector()
        self.failure = {"message": "Error while processing stat", "bucket": "autoscaler-bucket-00-of-10",
                        "knative.dev/key": "serving-tests/revision"}

    def test_persistent_forwarding_failure(self):
        self.assertIsNone(self.detector.record("autoscaler", self.failure, 0))
        self.assertIsNone(self.detector.record("autoscaler", self.failure, 5))
        event = self.detector.record("autoscaler", self.failure, 10)
        self.assertEqual(event["cause"], "forwarding")
        self.assertEqual(event["duration_seconds"], 10)
        self.assertEqual(event["count"], 3)

    def test_expected_pod_ip_failure_is_ignored(self):
        row = {"message": "Failed to establish stat forwarding connection", "address": "ws://10.0.0.1:8080"}
        for now in [0, 5, 10, 15]:
            self.assertIsNone(self.detector.record("autoscaler", row, now))
        self.assertFalse(self.detector.failures)

    def test_service_dns_failure_is_detected(self):
        row = {"message": "Failed to establish stat forwarding connection",
               "address": "ws://autoscaler-bucket-00-of-10.system.svc.cluster.local:8080"}
        self.detector.record("autoscaler", row, 0)
        self.detector.record("autoscaler", row, 5)
        self.assertEqual(self.detector.record("autoscaler", row, 10)["cause"], "forwarding")

    def test_brief_ha_failover_does_not_trigger(self):
        for now in [0, 1, 2, 3.4]:
            self.assertIsNone(self.detector.record("autoscaler", self.failure, now))
        self.assertIsNone(self.detector.record("autoscaler", self.failure, 20))

    def test_recovery_resets_failure_window(self):
        self.detector.record("autoscaler", self.failure, 0)
        self.detector.record("autoscaler", self.failure, 5)
        self.detector.record("autoscaler", {"message": "Stat forwarding connection established",
                                             "bucket": self.failure["bucket"]}, 8)
        self.assertIsNone(self.detector.record("autoscaler", self.failure, 10))

    def test_probes_require_ready_backends(self):
        row = {"message": "Failed to probe clusterIP 10.0.0.1:80", "knative.dev/key": "serving-tests/revision",
               "ready_backends": 0}
        for now in [0, 5, 10]:
            self.assertIsNone(self.detector.record("activator", row, now))
        row["ready_backends"] = 1
        self.detector.record("activator", row, 20)
        self.detector.record("activator", row, 25)
        self.assertEqual(self.detector.record("activator", row, 30)["cause"], "backend-probe")

    def test_stale_logs_are_not_live_failure_evidence(self):
        now = datetime.datetime.now(datetime.timezone.utc)
        self.assertTrue(recent({"timestamp": now.isoformat()}))
        self.assertFalse(recent({"timestamp": (now - datetime.timedelta(seconds=90)).isoformat()}))
        self.assertFalse(recent({"timestamp": "bad"}))


if __name__ == "__main__":
    unittest.main()
