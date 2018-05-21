/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package revision

import (
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const fluentdConfigSource = `
<source>
	@type tail
	path /var/log/revisions/**/*.*
	pos_file /var/log/varlog.log.pos
	tag *
	<parse>
		@type multi_format
		<pattern>
			format json
			time_key fluentd-time # fluentd-time is reserved for structured logs
			time_format %Y-%m-%dT%H:%M:%S.%NZ
		</pattern>
		<pattern>
			format none
			message_key log
		</pattern>
	</parse>
	read_from_head true
</source>

<filter var.log.**>
	@type record_transformer
	enable_ruby true
	<record>
	  kubernetes ${ {"container_name": "#{ENV['ELA_CONTAINER_NAME']}", "namespace_name": "#{ENV['ELA_NAMESPACE']}", "pod_name": "#{ENV['ELA_POD_NAME']}", "labels": {"elafros_dev/configuration": "#{ENV['ELA_CONFIGURATION']}", "elafros_dev/revision": "#{ENV['ELA_REVISION']}"} } }
		stream varlog
	</record>
</filter>

<match **>
	@type google_cloud

	# Try to detect JSON formatted log entries.
	detect_json true
	# Collect metrics in Prometheus registry about plugin activity.
	# enable_monitoring true
	# monitoring_type prometheus
	# Allow log entries from multiple containers to be sent in the same request.
	split_logs_by_tag false
	# Set the buffer type to file to improve the reliability and reduce the memory consumption
	buffer_type file
	buffer_path /var/log/fluentd-buffers/kubernetes.containers.buffer
	# Set queue_full action to block because we want to pause gracefully
	# in case of the off-the-limits load instead of throwing an exception
	buffer_queue_full_action block
	# Set the chunk limit conservatively to avoid exceeding the recommended
	# chunk size of 5MB per write request.
	buffer_chunk_limit 1M
	# Cap the combined memory usage of this buffer and the one below to
	# 1MiB/chunk * (6 + 2) chunks = 8 MiB
	buffer_queue_limit 6
	# Never wait more than 5 seconds before flushing logs in the non-error case.
	flush_interval 5s
	# Never wait longer than 30 seconds between retries.
	max_retry_wait 30
	# Disable the limit on the number of retries (retry forever).
	disable_retry_limit
	# Use multiple threads for processing.
	num_threads 2
	use_grpc true
</match>
`

const fluentdConfigMapName = "fluentd-varlog-config"

// MakeFluentdConfigMap creates a ConfigMap that gets mounted for fluentd
// container on the pod.
func MakeFluentdConfigMap(rev *v1alpha1.Revision, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      fluentdConfigMapName,
			Namespace: namespace,
			Labels:    MakeElaResourceLabels(rev),
		},
		Data: map[string]string{
			"varlog.conf": fluentdConfigSource,
		},
	}
}
