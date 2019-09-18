# Profiling Knative Serving

Knative Serving allows for collecting runtime profiling data expected by the
pprof visualization tool. Profiling data is available for the autoscaler,
activator, controller, webhook and for the queue-proxy container which is
injected into the user application pod. When enabled Knative serves profiling
data on the default port `8008` through a web server.

## Steps to get profiling data

Edit the `config-observability` ConfigMap and add `profiling.enable = "true"`:

```shell
kubectl edit configmap config-observability -n knative-serving
```

Use port-forwarding to get access to Knative Serving pods. For example,
activator:

```shell
ACTIVATOR_POD=$(kubectl -n knative-serving get pods -l app=activator -o custom-columns=:metadata.name --no-headers)
kubectl port-forward -n knative-serving $ACTIVATOR_POD 8008:8008
```

View all available profiles at http://localhost:8008/debug/pprof/ through a web
browser or request specific profiling data using one of the commands below:

### Heap profile

```shell
go tool pprof http://localhost:8008/debug/pprof/heap
```

### 30-second CPU profile

```shell
go tool pprof http://localhost:8008/debug/pprof/profile?seconds=30
```

### Go routine blocking profile

```shell
go tool pprof http://localhost:8008/debug/pprof/block
```

### 5-second execution trace

```shell
wget http://localhost:8008/debug/pprof/trace\?seconds\=5 && go tool trace trace\?seconds\=5
```

### All memory allocations

```shell
go tool pprof http://localhost:8008/debug/pprof/allocs
```

### Holders of contended mutexes

```shell
go tool pprof http://localhost:8008/debug/pprof/mutex
```

### Stack traces of all current goroutines

```shell
go tool pprof http://localhost:8008/debug/pprof/goroutine
```

### Stack traces that led to the creation of new OS threads

```shell
go tool pprof http://localhost:8008/debug/pprof/threadcreate
```

### Command line arguments for the current program

```shell
curl http://localhost:8008/debug/pprof/cmdline --output -
```

More information on profiling Go applications in this
[blog](https://blog.golang.org/profiling-go-programs)
