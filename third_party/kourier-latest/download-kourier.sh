#!/usr/bin/env bash

set -ex

# Download Kourier
KOURIER_VERSION=0.2.3
KOURIER_YAML=kourier-knative.yaml
DOWNLOAD_URL=https://raw.githubusercontent.com/3scale/kourier/v${KOURIER_VERSION}/deploy/${KOURIER_YAML}

wget ${DOWNLOAD_URL}

cat <<EOF > kourier.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kourier-system
---
EOF

cat ${KOURIER_YAML} \
  `# Install Kourier into the kourier-system namespace` \
  | sed 's/namespace: knative-serving/namespace: kourier-system/' \
  `# Expose Kourier services with LoadBalancer IPs instead of ClusterIP` \
  | sed 's/ClusterIP/LoadBalancer/' \
  >> kourier.yaml

# Clean up.
rm ${KOURIER_YAML}
