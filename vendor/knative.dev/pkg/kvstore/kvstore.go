/*
Copyright 2020 The Knative Authors

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

package kvstore

import (
	"context"
)

type Interface interface {
	// Init loads the configstore from the backing store if it exists, or
	// if it does not, will create an empty one.
	Init(ctx context.Context) error
	// Load loads the configstore from the backing store
	Load(ctx context.Context) error
	// Save saves the configstore to the backing store
	Save(ctx context.Context) error
	// Get gets the key from the KVStore into the provided value
	Get(ctx context.Context, key string, value interface{}) error
	// Set sets the key into the KVStore from the provided value
	Set(ctx context.Context, key string, value interface{}) error
}
