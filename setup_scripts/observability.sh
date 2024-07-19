helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm install prometheus prometheus-community/kube-prometheus-stack -n default -f values.yaml
kubectl apply -f https://raw.githubusercontent.com/knative-sandbox/monitoring/main/servicemonitor.yaml
kubectl apply -f https://raw.githubusercontent.com/knative-sandbox/monitoring/main/grafana/dashboards.yaml
