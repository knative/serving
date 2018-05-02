# k8sflag
Dynamic flag-style bindings for Kubernetes ConfigMaps.

## Example

`config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: hello-config
data:
  hello.name: "world"
```

`pod.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hello-pod
spec:
  containers:
      ...
      volumeMounts:
        - name: hello-config-volume
          mountPath: /etc/config
  volumes:
    - name: hello-config-volume
      configMap:
        name: hello-config
```

`hello.go`:

```go
var name = k8sflag.String("hello.name", "nobody")

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello %v.\n", name.Get())
}

func main() {
	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)
}
```
