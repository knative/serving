# Profiling Knative Serving

Knative Serving allows for collecting runtime profiling data expected
by the pprof visualization tool. Profiling data is available for the autoscaler,
activator, controller, and webhook containers and for the queue-proxy
container which is injected into the user application pod. When enabled
Knative serves profiling data on the default port 8008 through a web server.

## Steps to get profiling data:

* Edit the `config-observability` ConfigMap and add `profiling.enable = "true"`:

        oc edit configmap config-observability -n knative-serving

* Use port-forwarding to get access to Knative Serving pods. For example, activator:

  ```
    ACTIVATOR_POD=$(oc get pods -n knative-serving | grep activator | awk '{print $1}')

    kubectl port-forward -n knative-serving $ACTIVATOR_POD 8008:8008
  ```

* View all available profiles at http://localhost:8008/debug/pprof/ through a web browser or
   request specific profiling data using one of the commands below:

  a) Heap profile

        go tool pprof http://localhost:8008/debug/pprof/heap

  b) 30-second CPU profile

        go tool pprof http://localhost:8008/debug/pprof/profile?seconds=30

  c) Go routine blocking profile

        go tool pprof http://localhost:8008/debug/pprof/block

  d) 5-second execution trace

        wget http://localhost:8008/debug/pprof/trace\?seconds\=5 && go tool trace trace\?seconds\=5

  e) All memory allocations

        go tool pprof http://localhost:8008/debug/pprof/allocs

  f) Holders of contended mutexes

        go tool pprof http://localhost:8008/debug/pprof/mutex

  g) Stack traces of all current goroutines

        go tool pprof http://localhost:8008/debug/pprof/goroutine

  h) Stack traces that led to the creation of new OS threads

        go tool pprof http://localhost:8008/debug/pprof/threadcreate

  i) Command line arguments for the current program

        curl http://localhost:8008/debug/pprof/cmdline --output -

More information on profiling Go applications in this [blog](https://blog.golang.org/profiling-go-programs)
