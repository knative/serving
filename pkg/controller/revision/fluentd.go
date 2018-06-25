/*
Copyright 2018 The Knative Authors

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
	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fluentdSidecarPreOutputConfig defines source and filter configurations for
// files under /var/log.
const fluentdSidecarPreOutputConfig = `
<source>
	@type tail
	path /var/log/revisions/**/*.*
	pos_file /var/log/varlog.log.pos
	tag raw.*
	format none
	message_key log
	read_from_head true
</source>

<filter raw.var.log.**>
	@type record_transformer
	enable_ruby true
	<record>
	  kubernetes ${ {"container_name": "#{ENV['ELA_CONTAINER_NAME']}", "namespace_name": "#{ENV['ELA_NAMESPACE']}", "pod_name": "#{ENV['ELA_POD_NAME']}", "labels": {"knative_dev/configuration": "#{ENV['ELA_CONFIGURATION']}", "knative_dev/revision": "#{ENV['ELA_REVISION']}"} } }
		stream varlog
		# Line breaks may be trimmed when collecting from files. Add them back so that
		# multi line logs are still in multi line after combined by detect_exceptions.
		# Remove this if https://github.com/GoogleCloudPlatform/fluent-plugin-detect-exceptions/pull/10 is released
		log ${ if record["log"].end_with?("\n") then record["log"] else record["log"] + "\n" end }
	</record>
</filter>

<match raw.var.log.**>
	@id raw.var.log
	@type detect_exceptions
	remove_tag_prefix raw
	message log
	stream stream
	multiline_flush_interval 5
	max_bytes 500000
	max_lines 1000
</match>

`

const fluentdConfigMapName = "fluentd-varlog-config"

// MakeFluentdConfigMap creates a ConfigMap that gets mounted for fluentd
// container on the pod.
func MakeFluentdConfigMap(
	namespace string, fluentdSidecarOutputConfig string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      fluentdConfigMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			"varlog.conf": makeFullFluentdConfig(fluentdSidecarOutputConfig),
		},
	}
}

func makeFullFluentdConfig(fluentdSidecarOutputConfig string) string {
	return fluentdSidecarPreOutputConfig + fluentdSidecarOutputConfig
}
