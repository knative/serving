function main() {
  parse_args "$@"

  # Wait for the Ingress.
  while true; do
    INGRESS_IP=$(kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}")
    if [ ! -z "$INGRESS_IP" ]; then
      break
    fi
    echo "Sleeping 5 secs waiting for IP address"
    sleep 5
  done

  echo "Found Ingress at $INGRESS_IP"

  #  Wait for Route.
  while true; do
    ROUTE_DOMAIN=$(kubectl get route -n $ROUTE_NS $ROUTE_NAME -o jsonpath="{.status.domain}")
    if [ ! -z "$ROUTE_DOMAIN" ]; then
      break
    fi
    echo "Sleeping 5 secs waiting for route to be ready"
    sleep 5
  done
  echo "Route is ready for serving domain $ROUTE_DOMAIN"

  # Curl'ing the Ingress.
  echo "Running \`curl --resolve $ROUTE_DOMAIN:80:$INGRESS_IP http://${ROUTE_DOMAIN}${URL_PATH}\`"


  LAST_STATUS="Unknown"
  CURRENT_STATUS="Unknown"

  while true; do
    CURRENT_STATUS=$(curl --resolve $ROUTE_DOMAIN:80:$INGRESS_IP http://${ROUTE_DOMAIN}${URL_PATH} -w "[%{http_code}]\n" --silent | tr '\n' ' ')
    if [ "$CURRENT_STATUS" != "$LAST_STATUS" ]; then
      echo "$(date): ${LAST_STATUS} -> ${CURRENT_STATUS}"
    fi
    LAST_STATUS=$CURRENT_STATUS
  done
}

# Print usage message.
function exit_with_usage() {
  cat <<EOF
Usage:  query-route.sh <options>
  --route,     -r <route>     Route to curl. Required.
  --path,      -p <path>      Path to curl.  Default to ''.
  --namespace, -n <namespace> Namespace of the route.  Default to 'default'.
  --help,      -h             Print this message.
EOF
  exit -1
}

# Parse command line arguments.
function parse_args() {
  while (( "$#" )); do
    case "$1" in
      -p|--path)
        URL_PATH=$2
        shift 2
        ;;
      -r|--route)
        ROUTE_NAME=$2
        shift 2
        ;;
      -n|--namespace)
        ROUTE_NS=$2
        shift 2
        ;;
      --) # end argument parsing
        shift
        break
        ;;
      -h|--help)
        exit_with_usage
        ;;
      *|-*|--*=) # unsupported flags
      echo "Error: Unsupported flag $1" >&2
      exit_with_usage
      ;;
      *) # preserve positional arguments
        echo "Error: Unsupported positional arguments $1" >&2
        exit_with_usage
    esac
  done

  # Complain about required parameters.
  if [ -z "$ROUTE_NAME" ]; then
    exit_with_usage
  fi

  # Use namespace default if unset.
  : ${ROUTE_NS:="default"}
  : ${URL_PATH:=""}

  export ROUTE_NS
  export URL_PATH
  export ROUTE_NAME
}

main $@
