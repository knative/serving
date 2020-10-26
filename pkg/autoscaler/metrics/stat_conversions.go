/*
Copyright 2020 The Knative Authors

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

package metrics

import "k8s.io/apimachinery/pkg/types"

// ToWireStatMessage converts the StatMessage to a WireStatMessage.
func (sm StatMessage) ToWireStatMessage() *WireStatMessage {
	return &WireStatMessage{
		Namespace: sm.Key.Namespace,
		Name:      sm.Key.Name,
		Stat:      &sm.Stat,
	}
}

// ToStatMessage converts the WireStatMessage to a StatMessage.
// Nil-checks must have been done before calling this.
func (wsm WireStatMessage) ToStatMessage() StatMessage {
	return StatMessage{
		Key: types.NamespacedName{
			Namespace: wsm.Namespace,
			Name:      wsm.Name,
		},
		Stat: *wsm.Stat,
	}
}

// ToWireStatMessages converts the given slice of StatMessages to a WireStatMessages
// struct, ready to be sent off.
func ToWireStatMessages(sms []StatMessage) WireStatMessages {
	wsms := WireStatMessages{
		Messages: make([]*WireStatMessage, len(sms)),
	}
	for i, sm := range sms {
		wsms.Messages[i] = sm.ToWireStatMessage()
	}
	return wsms
}
