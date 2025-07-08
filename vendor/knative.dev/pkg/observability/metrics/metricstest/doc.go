/*
Copyright 2025 The Knative Authors

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

/*
Package metricstest provides test helpers to assert OTel metrics
are recorded properly.

When setting up your package for unit tests it is recommended that
your instrumentation can be reset between tests. This can be accomplished
using the following structure.

	func init() {
		resetPackageMetrics()
	}

	var instrument metric.Int64Counter

	func resetPackageMetrics() {
		// Reset the meter
		var meter = otel.Meter("com.some.scope")

		// Reset the instrumentation
		instrument = meter.Int64Counter("some.metric")
	}

Then in your tests you can setup the metrics pipeline as follows:

	func TestMetricsRecorded(t *testing.T) {
		reader := metric.NewManualReader()
		provider := metric.NewMeterProvider(metric.WithReader(reader))

		// Set the global provider
		otel.SetMeterProvider(provider)

		// This rebuilds instruments with the new provider and directs
		// the metrics to be recorded in the manual reader declared above.
		resetPackageMetrics()

		doSomething()


		metricstest.AssertMetrics(t, reader,
			metricstest.MetricPresent(...),
			metricstest.HasAttributes(...),
		)
	}
*/
package metricstest
