mkdir -p ${ARTIFACTS}/dump
while true; do
  kubectl exec $(kubectl -n istio-system get pods -lapp=istio-ingressgateway -oname) -n istio-system -- curl -s localhost:15000/config_dump > ${ARTIFACTS}/dump/config-$(date  "+%FT%T.%N")
  kubectl exec $(kubectl -n istio-system get pods -lapp=istio-ingressgateway -oname) -n istio-system -- curl -s localhost:15000/clusters > ${ARTIFACTS}/dump/clusters-$(date  "+%FT%T.%N")
  echo "dump done"
done
