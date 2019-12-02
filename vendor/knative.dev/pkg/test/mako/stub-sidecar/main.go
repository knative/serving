/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang/protobuf/jsonpb"
	mako "github.com/google/mako/spec/proto/mako_go_proto"

	"log"
	"net"
	"sync"

	"google.golang.org/grpc"

	"knative.dev/pkg/test/mako/config"
	qspb "knative.dev/pkg/third_party/mako/proto/quickstore_go_proto"
)

const (
	port                               = ":9813"
	defaultServerMaxReceiveMessageSize = 1024 * 1024 * 1024
)

type server struct {
	info     *mako.BenchmarkInfo
	stopOnce sync.Once
	stopCh   chan struct{}
	sb       *strings.Builder
}

func (s *server) Store(ctx context.Context, in *qspb.StoreInput) (*qspb.StoreOutput, error) {
	m := jsonpb.Marshaler{}
	qi, _ := m.MarshalToString(in.GetQuickstoreInput())
	fmt.Printf("# Received input")

	fmt.Fprintf(s.sb, "# %s\n", qi)
	writer := csv.NewWriter(s.sb)

	kv := calculateKeyIndexColumnsMap(s.info)
	cols := make([]string, len(kv))
	for k, i := range kv {
		cols[i] = k
	}
	fmt.Fprintf(s.sb, "# %s\n", strings.Join(cols, ","))

	for _, sp := range in.GetSamplePoints() {
		for _, mv := range sp.GetMetricValueList() {
			vals := map[string]string{"inputValue": fmt.Sprintf("%f", sp.GetInputValue())}
			vals[mv.GetValueKey()] = fmt.Sprintf("%f", mv.GetValue())
			writer.Write(makeRow(vals, kv))
		}
	}

	for _, ra := range in.GetRunAggregates() {
		vals := map[string]string{ra.GetValueKey(): fmt.Sprintf("%f", ra.GetValue())}
		writer.Write(makeRow(vals, kv))
	}

	for _, sa := range in.GetSampleErrors() {
		vals := map[string]string{"inputValue": fmt.Sprintf("%f", sa.GetInputValue()), "errorMessage": sa.GetErrorMessage()}
		writer.Write(makeRow(vals, kv))
	}

	writer.Flush()

	fmt.Fprintf(s.sb, "# CSV end\n")
	fmt.Printf("# Input completed")

	return &qspb.StoreOutput{}, nil
}

func makeRow(points map[string]string, kv map[string]int) []string {
	row := make([]string, len(kv))
	for k, v := range points {
		row[kv[k]] = v
	}
	return row
}

func calculateKeyIndexColumnsMap(info *mako.BenchmarkInfo) map[string]int {
	kv := make(map[string]int)
	kv["inputValue"] = 0
	kv["errorMessage"] = 1
	for i, m := range info.MetricInfoList {
		kv[*m.ValueKey] = i + 2
	}
	return kv
}

func (s *server) ShutdownMicroservice(ctx context.Context, in *qspb.ShutdownInput) (*qspb.ShutdownOutput, error) {
	s.stopOnce.Do(func() { close(s.stopCh) })
	return &qspb.ShutdownOutput{}, nil
}

var httpPort int

func init() {
	flag.IntVar(&httpPort, "p", 0, "Port to use for using stub in HTTP mode. 0 means print to logs and quit")
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(grpc.MaxRecvMsgSize(defaultServerMaxReceiveMessageSize))
	stopCh := make(chan struct{})
	info := config.MustGetBenchmark()
	var sb strings.Builder

	fmt.Fprintf(&sb, "# Benchmark %s - %s\n", *info.BenchmarkKey, *info.BenchmarkName)

	go func() {
		qspb.RegisterQuickstoreServer(s, &server{info: info, stopCh: stopCh, sb: &sb})
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	<-stopCh
	s.GracefulStop()

	results := sb.String()

	if httpPort != 0 {
		m := http.NewServeMux()
		s := http.Server{Addr: fmt.Sprintf(":%d", httpPort), Handler: m}

		m.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/csv")
			_, err := fmt.Fprint(w, results)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		})
		m.HandleFunc("/close", func(writer http.ResponseWriter, request *http.Request) {
			s.Shutdown(context.Background())
		})
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		fmt.Print("Successfully served the results")
	} else {
		fmt.Print(sb.String())
	}
}
