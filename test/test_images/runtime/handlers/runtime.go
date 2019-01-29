/*
Copyright 2019 The Knative Authors
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

package handlers

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/knative/serving/test/types"
)

// filePaths is the set of filepaths probed and returned to the
// client as FileInfo.
var filePaths = []string{"/tmp",
	"/var/log",
	"/dev/log",
	"/etc/hosts",
	"/etc/hostname",
	"/etc/resolv.conf"}

// cgroupPaths is the set of cgroups probed and returned to the
// client as Cgroups.
var cgroupPaths = []string{
	"/sys/fs/cgroup/memory/memory.limit_in_bytes",
	"/sys/fs/cgroup/cpu/cpu.cfs_period_us",
	"/sys/fs/cgroup/cpu/cpu.cfs_quota_us",
	"/sys/fs/cgroup/cpu/cpu.shares"}

func fileInfo(paths ...string) []*types.FileInfo {
	var infoList []*types.FileInfo

	for _, path := range paths {
		file, err := os.Stat(path)
		if err != nil {
			infoList = append(infoList, &types.FileInfo{Name: path, Error: err.Error()})
			continue
		}
		size := file.Size()
		dir := file.IsDir()
		infoList = append(infoList, &types.FileInfo{Name: path,
			Size:    &size,
			Mode:    file.Mode().String(),
			ModTime: file.ModTime(),
			IsDir:   &dir})
	}
	return infoList
}

func env() map[string]string {
	envMap := map[string]string{}
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		envMap[pair[0]] = pair[1]
	}
	return envMap
}

func headers(r *http.Request) map[string]string {
	headerMap := map[string]string{}
	for name, headers := range r.Header {
		name = strings.ToLower(name)
		for i, h := range headers {
			if len(headers) > 1 {
				headerMap[fmt.Sprintf("%s[%d]", name, i)] = h
			} else {
				headerMap[fmt.Sprintf("%s", name)] = h
			}
		}
	}
	return headerMap
}

func requestInfo(r *http.Request) *types.RequestInfo {
	return &types.RequestInfo{
		Ts:      time.Now(),
		URI:     r.RequestURI,
		Host:    r.Host,
		Method:  r.Method,
		Headers: headers(r),
	}
}

func cgroups(paths ...string) []*types.Cgroup {
	var cgroups []*types.Cgroup
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			cgroups = append(cgroups, &types.Cgroup{Name: path, Error: err.Error()})
			continue
		}

		bc, err := ioutil.ReadFile(path)
		if err != nil {
			cgroups = append(cgroups, &types.Cgroup{Name: path, Error: err.Error()})
			continue
		}
		cs := strings.Trim(string(bc), "\n")
		ic, err := strconv.Atoi(string(cs))
		if err != nil {
			cgroups = append(cgroups, &types.Cgroup{Name: path, Error: err.Error()})
			continue
		}

		// Try to write to the Cgroup. We expect this to fail as a cheap
		// method for read-only validation
		newValue := []byte{'9'}
		err = ioutil.WriteFile(path, newValue, 420)
		t := true
		f := false
		if err != nil {
			cgroups = append(cgroups, &types.Cgroup{Name: path, Value: &ic, ReadOnly: &t})
		} else {
			cgroups = append(cgroups, &types.Cgroup{Name: path, Value: &ic, ReadOnly: &f})
		}
	}
	return cgroups
}

func runtimeHandler(w http.ResponseWriter, r *http.Request) {

	log.Println("Retrieiving Runtime Information")
	w.Header().Set("Content-Type", "application/json")

	k := &types.RuntimeInfo{
		Request: requestInfo(r),
		Host: &types.HostInfo{EnvVars: env(),
			Files:   fileInfo(filePaths...),
			Cgroups: cgroups(cgroupPaths...),
		},
	}

	writeJSON(w, k)

}
