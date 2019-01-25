# Setting Up Custom Ingress Gateway

Knative uses a shared Gateway to serve all incoming traffic within Knative
service mesh, which is the "knative-shared-gateway" Gateway under
"knative-serving" namespace. By default, we use Istio gateway service
`istio-ingressgateway` under "istio-system" namespace as its underlying service.
You can replace the service with that of your own as follows.

## Step 1: Create Gateway Service and Deployment Instance

You'll need to create the gateway service and deployment instance to handle
traffic first. The simplest way should be making a copy of the Gateway service
template in [Istio release](https://github.com/istio/istio/releases).

Here is an example:

```
apiVersion: v1
kind: Service
metadata:
  name: custom-ingressgateway
  namespace: istio-system
  annotations:
  labels:
    chart: gateways-1.0.1
    release: RELEASE-NAME
    heritage: Tiller
    app: custom-ingressgateway
    custom: ingressgateway
spec:
  type: LoadBalancer
  selector:
    app: custom-ingressgateway
    custom: ingressgateway
  ports:
    -
      name: http2
      nodePort: 32380
      port: 80
      targetPort: 80
    -
      name: https
      nodePort: 32390
      port: 443
    -
      name: tcp
      nodePort: 32400
      port: 31400
    -
      name: tcp-pilot-grpc-tls
      port: 15011
      targetPort: 15011
    -
      name: tcp-citadel-grpc-tls
      port: 8060
      targetPort: 8060
    -
      name: tcp-dns-tls
      port: 853
      targetPort: 853
    -
      name: http2-prometheus
      port: 15030
      targetPort: 15030
    -
      name: http2-grafana
      port: 15031
      targetPort: 15031
---
# This is the corresponding Deployment to backed the aforementioned Service.
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: custom-ingressgateway
  namespace: istio-system
  labels:
    chart: gateways-1.0.1
    release: RELEASE-NAME
    heritage: Tiller
    app: custom-ingressgateway
    custom: ingressgateway
spec:
  replicas: 1
  selector:
    matchLabels:
      app: custom-ingressgateway
      custom: ingressgateway
  template:
    metadata:
      labels:
        app: custom-ingressgateway
        custom: ingressgateway
      annotations:
        sidecar.istio.io/inject: "false"
        scheduler.alpha.kubernetes.io/critical-pod: ""
    spec:
      serviceAccountName: istio-ingressgateway-service-account
      containers:
        - name: istio-proxy
          image: "docker.io/istio/proxyv2:1.0.2"
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 80
            - containerPort: 443
            - containerPort: 31400
            - containerPort: 15011
            - containerPort: 8060
            - containerPort: 853
            - containerPort: 15030
            - containerPort: 15031
          args:
          - proxy
          - router
          - -v
          - "2"
          - --discoveryRefreshDelay
          - '1s' #discoveryRefreshDelay
          - --drainDuration
          - '45s' #drainDuration
          - --parentShutdownDuration
          - '1m0s' #parentShutdownDuration
          - --connectTimeout
          - '10s' #connectTimeout
          - --serviceCluster
          - custom-ingressgateway
          - --zipkinAddress
          - zipkin:9411
          - --statsdUdpAddress
          - istio-statsd-prom-bridge:9125
          - --proxyAdminPort
          - "15000"
          - --controlPlaneAuthPolicy
          - NONE
          - --discoveryAddress
          - istio-pilot:8080
          resources:
            requests:
              cpu: 10m
          env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: metadata.namespace
          - name: INSTANCE_IP
            valueFrom:
              fieldRef:
                apiVersion: v1
                fieldPath: status.podIP
          - name: ISTIO_META_POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          volumeMounts:
          - name: istio-certs
            mountPath: /etc/certs
            readOnly: true
          - name: ingressgateway-certs
            mountPath: "/etc/istio/ingressgateway-certs"
            readOnly: true
          - name: ingressgateway-ca-certs
            mountPath: "/etc/istio/ingressgateway-ca-certs"
            readOnly: true
      volumes:
      - name: istio-certs
        secret:
          secretName: istio.istio-ingressgateway-service-account
          optional: true
      - name: ingressgateway-certs
        secret:
          secretName: "istio-ingressgateway-certs"
          optional: true
      - name: ingressgateway-ca-certs
        secret:
          secretName: "istio-ingressgateway-ca-certs"
          optional: true
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: beta.kubernetes.io/arch
                operator: In
                values:
                - amd64
                - ppc64le
                - s390x
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 2
            preference:
              matchExpressions:
              - key: beta.kubernetes.io/arch
                operator: In
                values:
                - amd64
          - weight: 2
            preference:
              matchExpressions:
              - key: beta.kubernetes.io/arch
                operator: In
                values:
                - ppc64le
          - weight: 2
            preference:
              matchExpressions:
              - key: beta.kubernetes.io/arch
                operator: In
                values:
                - s390x
```

## Step 2: Update Knative Gateway

Update gateway instance `knative-shared-gateway` under `knative-serving`
namespace:

```shell
kubectl edit gateway knative-shared-gateway -n knative-serving
```

Replace its label selector with the label of your service:

```
istio: ingressgateway
```

For the service above, it should be updated to

```
custom: ingressgateway
```

If there is a change in service ports (compared with that of
`istio-ingressgateway`), update the port info in gateway accordingly.

## Step 3: Update Gateway Configmap

Update gateway configmap `config-ingressgateway` under `knative-serving`
namespace:

```shell
kubectl edit configmap config-ingressgateway -n knative-serving
```

Replace the `ingress-gateway` field with fully qualified url of your service:

For the service above, it should be updated to

```
custom-ingressgateway.istio-system.svc.cluster.local
```
