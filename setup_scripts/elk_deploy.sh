kubectl apply -f local-path-storage.yaml
kubectl patch storageclass local-path -p '{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'
helm install elasticsearch elastic/elasticsearch --version 7.17.3 -f values_es.yaml
helm install kibana elastic/kibana --version 7.17.3 -f values_kb.yaml
helm install fluent fluent/fluent-bit -f values_fb.yaml
kubectl apply -f fluent-bit-collector.yaml
