/*
Autoscaler calculates the number of pods necessary for the desired
level of concurrency per pod (stableConcurrencyPerPod). It operates in
two modes, stable mode and panic mode.

Stable mode calculates the average concurrency observed over the last
60 seconds and adjusts the observed pod count to achieve the target
value. Current observed pod count is the number of unique pod names
which show up in the last 60 seconds.

Panic mode calculates the average concurrency observed over the last 6
seconds and adjusts the observed pod count to achieve the stable
target value. Panic mode is engaged when the observed 6 second average
concurrency reaches 2x the target stable concurrency. Panic mode will
last at least 60 seconds--longer if the 2x threshold is repeatedly
breached. During panic mode the number of pods is never decreased in
order to prevent flapping.
*/
package autoscaler
