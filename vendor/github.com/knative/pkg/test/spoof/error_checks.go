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

// spoof contains logic to make polling HTTP requests against an endpoint with optional host spoofing.

package spoof

import (
	"net"
	"net/url"
	"strings"
)

func isTCPTimeout(e error) bool {
	err, ok := e.(net.Error)
	return err != nil && ok && err.Timeout()
}

func isDNSError(err error) bool {
	if err, ok := err.(*url.Error); err != nil && ok {
		if err, ok := err.Err.(*net.OpError); err != nil && ok {
			if err, ok := err.Err.(*net.DNSError); err != nil && ok {
				return true
			}
		}
	}
	return false
}

func isTCPConnectRefuse(err error) bool {
	// The alternative for the string check is:
	// 	errNo := (((err.(*url.Error)).Err.(*net.OpError)).Err.(*os.SyscallError).Err).(syscall.Errno)
	// if errNo == syscall.Errno(0x6f) {...}
	// But with assertions, of course.
	if err != nil && strings.Contains(err.Error(), "connect: connection refused") {
		return true
	}
	return false
}
