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

package transport

import (
	"github.com/ease-lab/XDTprototype/utils"
	log "github.com/sirupsen/logrus"
	"sync"
)

type BufferPool struct {
	bufferChannels chan (chan []byte)
	channelMap     sync.Map
}

type buffer struct {
	channel     chan []byte
	totalChunks int64
}

func (b *BufferPool) Init(config utils.Config) {

	b.bufferChannels = make(chan chan []byte, config.NumberOfBuffers)

	var bufferSize int

	if config.Routing == utils.CUT_THROUGH {
		bufferSize = config.CTBufferSize
	} else if config.Routing == utils.STORE_FORWARD {
		bufferSize = config.StAndFwBufferSize
	} else {
		log.Fatalf("transport: Invalid route type. Check config.json")
	}

	for i := 0; i < config.NumberOfBuffers; i++ {
		tmpChannel := make(chan []byte, bufferSize)
		b.bufferChannels <- tmpChannel
	}
}

func (b *BufferPool) CreateChannel() chan []byte {
	log.Infof("%d free channels available", len(b.bufferChannels))
	channel := <-b.bufferChannels
	return channel
}

func (b *BufferPool) StoreChannel(key string, totalChunks int64, channel chan []byte) {
	b.channelMap.Store(key, buffer{channel, totalChunks})
}

func (b *BufferPool) GetChannel(key string) (chan []byte, int64) {
	if tmpChanel, ok := b.channelMap.Load(key); ok {
		return tmpChanel.(buffer).channel, tmpChanel.(buffer).totalChunks
	} else {
		return nil, -1
	}
}

func (b *BufferPool) FreeChannel(key string) {
	if tmpChanel, ok := b.channelMap.Load(key); ok {
		b.channelMap.Delete(key)
		b.bufferChannels <- tmpChanel.(buffer).channel
	} else {
		log.Fatalf("sQP: %s key not found in buffer pool for deletion", key)
	}
}
