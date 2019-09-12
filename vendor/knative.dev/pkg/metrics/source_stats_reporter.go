/*
 * Copyright 2019 The Knative Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package metrics

import (
	"context"
	"strconv"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"knative.dev/pkg/metrics/metricskey"
)

var (
	// eventCountM is a counter which records the number of events sent by the source.
	eventCountM = stats.Int64(
		"event_count",
		"Number of events sent",
		stats.UnitDimensionless,
	)
)

type ReportArgs struct {
	Namespace     string
	EventType     string
	EventSource   string
	Name          string
	ResourceGroup string
}

// StatsReporter defines the interface for sending source metrics.
type StatsReporter interface {
	// ReportEventCount captures the event count. It records one per call.
	ReportEventCount(args *ReportArgs, responseCode int) error
}

var _ StatsReporter = (*reporter)(nil)

// reporter holds cached metric objects to report source metrics.
type reporter struct {
	namespaceTagKey           tag.Key
	eventSourceTagKey         tag.Key
	eventTypeTagKey           tag.Key
	sourceNameTagKey          tag.Key
	sourceResourceGroupTagKey tag.Key
	responseCodeKey           tag.Key
	responseCodeClassKey      tag.Key
}

// NewStatsReporter creates a reporter that collects and reports source metrics.
func NewStatsReporter() (StatsReporter, error) {
	var r = &reporter{}

	// Create the tag keys that will be used to add tags to our measurements.
	nsTag, err := tag.NewKey(metricskey.LabelNamespaceName)
	if err != nil {
		return nil, err
	}
	r.namespaceTagKey = nsTag

	eventSourceTag, err := tag.NewKey(metricskey.LabelEventSource)
	if err != nil {
		return nil, err
	}
	r.eventSourceTagKey = eventSourceTag

	eventTypeTag, err := tag.NewKey(metricskey.LabelEventType)
	if err != nil {
		return nil, err
	}
	r.eventTypeTagKey = eventTypeTag

	nameTag, err := tag.NewKey(metricskey.LabelImporterName)
	if err != nil {
		return nil, err
	}
	r.sourceNameTagKey = nameTag

	resourceGroupTag, err := tag.NewKey(metricskey.LabelImporterResourceGroup)
	if err != nil {
		return nil, err
	}
	r.sourceResourceGroupTagKey = resourceGroupTag

	responseCodeTag, err := tag.NewKey(metricskey.LabelResponseCode)
	if err != nil {
		return nil, err
	}
	r.responseCodeKey = responseCodeTag
	responseCodeClassTag, err := tag.NewKey(metricskey.LabelResponseCodeClass)
	if err != nil {
		return nil, err
	}
	r.responseCodeClassKey = responseCodeClassTag

	// Create view to see our measurements.
	err = view.Register(
		&view.View{
			Description: eventCountM.Description(),
			Measure:     eventCountM,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{r.namespaceTagKey, r.eventSourceTagKey, r.eventTypeTagKey, r.sourceNameTagKey, r.sourceResourceGroupTagKey, r.responseCodeKey, r.responseCodeClassKey},
		},
	)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *reporter) ReportEventCount(args *ReportArgs, responseCode int) error {
	ctx, err := r.generateTag(args, responseCode)
	if err != nil {
		return err
	}
	Record(ctx, eventCountM.M(1))
	return nil
}

func (r *reporter) generateTag(args *ReportArgs, responseCode int) (context.Context, error) {
	return tag.New(
		context.Background(),
		tag.Insert(r.namespaceTagKey, args.Namespace),
		tag.Insert(r.eventSourceTagKey, args.EventSource),
		tag.Insert(r.eventTypeTagKey, args.EventType),
		tag.Insert(r.sourceNameTagKey, args.Name),
		tag.Insert(r.sourceResourceGroupTagKey, args.ResourceGroup),
		tag.Insert(r.responseCodeKey, strconv.Itoa(responseCode)),
		tag.Insert(r.responseCodeClassKey, ResponseCodeClass(responseCode)))
}
