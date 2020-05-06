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

/*
Package autoscaler calculates the number of pods necessary for the desired
level of concurrency per pod. It operates in two modes, stable mode and panic
mode.

The stable mode is for general operation while the panic mode has, by default,
a much shorter window and will be used to quickly scale a revision up if a
burst of traffic comes in. As the panic window is a lot shorter, it will react
more quickly to load. The revision will not scale down while in panic mode to
avoid a lot of churn.
*/
package autoscaler
