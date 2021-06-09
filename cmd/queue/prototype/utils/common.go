// MIT License
//
// Copyright (c) 2021 Shyam Jesalpura and EASE lab
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package utils

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
)

type Config struct {
	ChunkSizeInBytes     int
	DQPServerAddr        string
	LBAddr               string
	DstServerAddr        string
	SQPServerAddr        string
	CTBufferSize         int
	NumberOfBuffers      int
	StAndFwBufferSize    int
	Routing              string
	TracingEnabled       bool
	RPCTimeoutMaxBackoff int
	RPCTimeoutDuration   int
	RPCRetryDelay        int
}

type Payload struct {
	FunctionName string
	Data         []byte
	Key          string
	IsXDT        bool
}

const (
	STORE_FORWARD = "Store&Forward"
	CUT_THROUGH   = "CutThrough"
)

var LoadConfig = readConfig("../config.json")

func readConfig(file string) Config {
	log.Debugf("Opening JSON file with config: %s\n", file)
	jsonFile, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		err = jsonFile.Close()
		if err != nil {
			log.Errorf("transport: Error closing the config file")
		}
	}()

	jsonByteValue, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		log.Fatal(err)
	}

	var config Config
	if err = json.Unmarshal(jsonByteValue, &config); err != nil {
		log.Fatal(err)
	}

	return config
}
