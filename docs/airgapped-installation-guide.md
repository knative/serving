# Installation Guide for Airgapped Environments

Installing Knative Serving in an airgapped (disconnected) environment requires careful preparation to ensure all necessary components are available locally. This guide walks you through the complete process step-by-step, helping you successfully deploy Knative Serving without internet connectivity.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Understanding the Components](#understanding-the-components)
- [Phase 1: Preparation on Internet-Connected Machine](#phase-1-preparation-on-internet-connected-machine)
- [Phase 2: Transfer to Airgapped Environment](#phase-2-transfer-to-airgapped-environment)
- [Phase 3: Installation in Airgapped Environment](#phase-3-installation-in-airgapped-environment)
- [Phase 4: Post-Installation Configuration](#phase-4-post-installation-configuration)
- [Verification and Troubleshooting](#verification-and-troubleshooting)
- [Additional Considerations](#additional-considerations)

## Overview

An airgapped environment is a network-isolated system that has no direct connection to the internet. Installing Knative Serving in such an environment requires:

1. **Downloading** all required files and container images on an internet-connected machine
2. **Transferring** these artifacts to your airgapped environment using secure methods
3. **Configuring** your private container registry to host the images
4. **Modifying** installation manifests to reference your local registry
5. **Installing** Knative Serving using the modified manifests

This guide assumes you have access to both an internet-connected machine (for downloading artifacts) and your airgapped Kubernetes cluster.

## Prerequisites

Before you begin, ensure you have the following:

### On Internet-Connected Machine

- **kubectl** (v1.15 or newer) - Kubernetes command-line tool
- **Docker** or compatible container runtime (containerd, podman)
- **curl** or **wget** - For downloading files
- **jq** (optional but recommended) - For parsing JSON
- **Sufficient disk space** - At least 10GB free for images and manifests

### In Airgapped Environment

- **Kubernetes cluster** (v1.15 or newer) - Fully functional cluster
- **kubectl** configured to access your cluster
- **Private container registry** - Accessible from all cluster nodes
  - Examples: Harbor, Artifactory, Nexus, or a simple Docker registry
  - Registry must support Docker v2 API
- **Network connectivity** between:
  - All Kubernetes nodes
  - Kubernetes nodes and the private registry
  - Your workstation and the Kubernetes cluster
- **Storage** - Sufficient space for container images (approximately 5-10GB)
- **Transfer mechanism** - Secure method to move files (USB drive, secure file transfer, etc.)

### Optional but Recommended

- **Helm** (v3.x) - Alternative installation method
- **Image mirroring tool** - Tools like `skopeo` or `crane` for efficient image transfer
- **Monitoring solution** - Prometheus, Grafana, or similar for observability

## Understanding the Components

Knative Serving consists of several core components that work together:

### Core Components

1. **Controller** - Manages the lifecycle of Knative resources (Services, Routes, Configurations, Revisions)
2. **Activator** - Handles traffic for scaled-to-zero services and manages cold starts
3. **Autoscaler** - Monitors traffic and scales pods up or down based on demand
4. **Webhook** - Validates and mutates Knative resources before they're created or updated
5. **Queue Proxy** - Sidecar container injected into each service pod for request queuing and metrics

### Post-Installation Jobs

1. **Default Domain** - Configures the default domain for your services
2. **Storage Version Migration** - Migrates existing resources to newer API versions
3. **Cleanup** - Performs maintenance tasks

### Networking Layer (Choose One)

Knative Serving requires a networking layer for ingress traffic. You can choose from:

- **Istio** - Full-featured service mesh (recommended for production)
- **Kourier** - Lightweight, Knative-native ingress (simpler setup)
- **Contour** - Envoy-based ingress controller
- **Gateway API** - Kubernetes-native API for service networking

### Optional Components

- **cert-manager** - For automatic TLS certificate management
- **HPA Autoscaling** - Horizontal Pod Autoscaler integration

## Phase 1: Preparation on Internet-Connected Machine

This phase involves downloading all necessary files and container images on a machine with internet access.

### Step 1.1: Choose Your Knative Serving Version

First, decide which version of Knative Serving you want to install. Check the [releases page](https://github.com/knative/serving/releases) for available versions.

```bash
# Set your desired version (example: v1.20.1)
export KNATIVE_VERSION="knative-v1.20.1"
```

**Recommendation**: Use a stable release version rather than the latest development version for production airgapped environments.

### Step 1.2: Create Working Directory

Create a directory to organize all downloaded artifacts:

```bash
mkdir -p knative-serving-airgap/${KNATIVE_VERSION}
cd knative-serving-airgap/${KNATIVE_VERSION}
mkdir -p manifests images scripts
```

### Step 1.3: Download Knative Serving Manifests

Download the required YAML manifest files:

```bash
# Core manifests
curl -LO "https://github.com/knative/serving/releases/download/${KNATIVE_VERSION}/serving-crds.yaml"
curl -LO "https://github.com/knative/serving/releases/download/${KNATIVE_VERSION}/serving-core.yaml"
curl -LO "https://github.com/knative/serving/releases/download/${KNATIVE_VERSION}/serving-default-domain.yaml"
curl -LO "https://github.com/knative/serving/releases/download/${KNATIVE_VERSION}/serving-post-install-jobs.yaml"

# Optional: HPA autoscaling
curl -LO "https://github.com/knative/serving/releases/download/${KNATIVE_VERSION}/serving-hpa.yaml"

# Move manifests to organized directory
mv *.yaml manifests/
```

### Step 1.4: Download Networking Layer

Choose and download your networking layer. Here are options for each:

#### Option A: Kourier (Recommended for Simplicity)

```bash
curl -LO "https://github.com/knative/net-kourier/releases/download/${KNATIVE_VERSION}/kourier.yaml"
mv kourier.yaml manifests/
```

#### Option B: Istio

```bash
# Download Istio (check latest version)
export ISTIO_VERSION="1.20.0"
curl -L "https://istio.io/downloadIstio" | sh -
cd istio-${ISTIO_VERSION}
# Istio installation files will be in this directory
```

#### Option C: Contour

```bash
curl -LO "https://github.com/knative/net-contour/releases/download/${KNATIVE_VERSION}/contour.yaml"
curl -LO "https://github.com/knative/net-contour/releases/download/${KNATIVE_VERSION}/net-contour.yaml"
mv contour.yaml net-contour.yaml manifests/
```

### Step 1.5: Extract Container Image References

Create a script to extract all container images from the YAML files:

```bash
cat > scripts/extract-images.sh << 'EOF'
#!/bin/bash
# Extract all image references from YAML files
grep -h "image:" manifests/*.yaml | \
  grep -v "^#" | \
  sed 's/.*image: *//' | \
  sed 's/^"//' | sed 's/"$//' | \
  sort -u > images/image-list.txt

echo "Found $(wc -l < images/image-list.txt) unique images"
cat images/image-list.txt
EOF

chmod +x scripts/extract-images.sh
./scripts/extract-images.sh
```

### Step 1.6: Download Container Images

Create a script to download all images:

```bash
cat > scripts/download-images.sh << 'EOF'
#!/bin/bash
set -e

REGISTRY_PREFIX="${1:-gcr.io/knative-releases}"
OUTPUT_DIR="images"

mkdir -p ${OUTPUT_DIR}

while IFS= read -r image; do
    # Handle ko:// references (these are build-time placeholders)
    if [[ "$image" == ko://* ]]; then
        echo "Skipping build-time reference: $image"
        continue
    fi
    
    # Extract image name and tag/digest
    image_name=$(echo "$image" | sed 's|.*/||' | cut -d: -f1 | cut -d@ -f1)
    
    echo "Pulling: $image"
    docker pull "$image" || {
        echo "Warning: Failed to pull $image"
        continue
    }
    
    # Save image to tar file
    safe_name=$(echo "$image" | tr '/:@' '___')
    docker save "$image" -o "${OUTPUT_DIR}/${safe_name}.tar"
    
    echo "Saved: ${OUTPUT_DIR}/${safe_name}.tar"
done < images/image-list.txt

echo "All images downloaded to ${OUTPUT_DIR}/"
EOF

chmod +x scripts/download-images.sh
./scripts/download-images.sh
```

**Note**: Some images may use `ko://` references which are build-time placeholders. These will be resolved when you build from source or use pre-built releases. For official releases, images are typically available from `gcr.io/knative-releases`.

### Step 1.7: Download Additional Dependencies

If you're using cert-manager or other optional components:

```bash
# cert-manager (if needed)
export CERT_MANAGER_VERSION="v1.13.0"
curl -LO "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
mv cert-manager.yaml manifests/
```

### Step 1.8: Create Image Transfer Script

Create a script that will help transfer images to your private registry:

```bash
cat > scripts/prepare-for-transfer.sh << 'EOF'
#!/bin/bash
# This script creates a comprehensive list of all artifacts

echo "=== Knative Serving Airgap Package ===" > TRANSFER_MANIFEST.txt
echo "Version: ${KNATIVE_VERSION}" >> TRANSFER_MANIFEST.txt
echo "Date: $(date)" >> TRANSFER_MANIFEST.txt
echo "" >> TRANSFER_MANIFEST.txt

echo "=== Manifests ===" >> TRANSFER_MANIFEST.txt
ls -lh manifests/ >> TRANSFER_MANIFEST.txt
echo "" >> TRANSFER_MANIFEST.txt

echo "=== Images ===" >> TRANSFER_MANIFEST.txt
ls -lh images/*.tar 2>/dev/null | wc -l | xargs echo "Total image files:" >> TRANSFER_MANIFEST.txt
du -sh images/ >> TRANSFER_MANIFEST.txt
echo "" >> TRANSFER_MANIFEST.txt

echo "=== Image List ===" >> TRANSFER_MANIFEST.txt
cat images/image-list.txt >> TRANSFER_MANIFEST.txt

cat TRANSFER_MANIFEST.txt
EOF

chmod +x scripts/prepare-for-transfer.sh
./scripts/prepare-for-transfer.sh
```

### Step 1.9: Verify Downloads

Before transferring, verify you have everything:

```bash
echo "=== Verification ==="
echo "Manifests:"
ls -1 manifests/*.yaml

echo ""
echo "Images:"
ls -1 images/*.tar | wc -l
echo "image files found"

echo ""
echo "Total size:"
du -sh .
```

## Phase 2: Transfer to Airgapped Environment

Transfer all artifacts from your internet-connected machine to the airgapped environment.

### Step 2.1: Package Artifacts

Create a compressed archive for easier transfer:

```bash
# From the parent directory
cd ..
tar -czf knative-serving-${KNATIVE_VERSION}-airgap.tar.gz \
  --exclude='*.tar' \
  knative-serving-airgap/${KNATIVE_VERSION}/manifests \
  knative-serving-airgap/${KNATIVE_VERSION}/scripts \
  knative-serving-airgap/${KNATIVE_VERSION}/images/image-list.txt \
  knative-serving-airgap/${KNATIVE_VERSION}/TRANSFER_MANIFEST.txt

# For images, you may want separate archives due to size
cd knative-serving-airgap/${KNATIVE_VERSION}
tar -czf ../knative-serving-${KNATIVE_VERSION}-images.tar.gz images/*.tar
```

### Step 2.2: Transfer Files

Use your organization's approved method to transfer files:

- **USB drive** - Most common for airgapped environments
- **Secure file transfer** - If a secure channel exists
- **Physical media** - DVDs, external drives, etc.

**Security Note**: Verify file integrity after transfer using checksums:

```bash
# On source machine
md5sum knative-serving-${KNATIVE_VERSION}-airgap.tar.gz > checksums.md5
md5sum knative-serving-${KNATIVE_VERSION}-images.tar.gz >> checksums.md5

# Transfer checksums.md5 as well, then verify on destination
```

### Step 2.3: Extract on Airgapped Machine

On your airgapped machine:

```bash
# Extract manifests and scripts
tar -xzf knative-serving-${KNATIVE_VERSION}-airgap.tar.gz

# Extract images
tar -xzf knative-serving-${KNATIVE_VERSION}-images.tar.gz

# Verify
cd knative-serving-airgap/${KNATIVE_VERSION}
cat TRANSFER_MANIFEST.txt
```

## Phase 3: Installation in Airgapped Environment

Now that all artifacts are in your airgapped environment, proceed with installation.

### Step 3.1: Configure Private Container Registry

Ensure your private registry is accessible and you have credentials:

```bash
# Test registry access
docker login <your-registry-host>:<port>

# Or for containerd
# ctr images pull <your-registry-host>:<port>/test:latest
```

### Step 3.2: Load Images into Private Registry

Create and run a script to load and push all images:

```bash
cat > scripts/load-and-push-images.sh << 'EOF'
#!/bin/bash
set -e

PRIVATE_REGISTRY="${1:?Usage: $0 <registry-host:port> [registry-namespace]}"
REGISTRY_NAMESPACE="${2:-knative}"

if [ -z "$PRIVATE_REGISTRY" ]; then
    echo "Error: Private registry address required"
    echo "Usage: $0 <registry-host:port> [registry-namespace]"
    exit 1
fi

echo "Loading and pushing images to ${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}"

for tar_file in images/*.tar; do
    if [ ! -f "$tar_file" ]; then
        echo "No tar files found in images/"
        break
    fi
    
    echo "Loading: $tar_file"
    docker load -i "$tar_file"
    
    # Get the image name that was loaded
    loaded_image=$(docker load -i "$tar_file" 2>&1 | grep "Loaded image" | sed 's/Loaded image: //')
    
    if [ -z "$loaded_image" ]; then
        # Try alternative method
        loaded_image=$(docker images --format "{{.Repository}}:{{.Tag}}" | head -1)
    fi
    
    # Extract original name and create new tag
    image_name=$(basename "$tar_file" .tar | tr '_' '/' | sed 's|knative-releases/knative.dev/||')
    
    # Create new tag for private registry
    new_tag="${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/${image_name##*/}"
    
    echo "Tagging: $loaded_image -> $new_tag"
    docker tag "$loaded_image" "$new_tag"
    
    echo "Pushing: $new_tag"
    docker push "$new_tag"
    
    echo "---"
done

echo "All images pushed to ${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}"
EOF

chmod +x scripts/load-and-push-images.sh

# Run the script
./scripts/load-and-push-images.sh <your-registry-host:port> knative
```

**Alternative**: If you have many images, consider using `skopeo` for more efficient transfers:

```bash
# Using skopeo (if available)
for tar_file in images/*.tar; do
    skopeo copy docker-archive:$tar_file docker://${PRIVATE_REGISTRY}/knative/$(basename $tar_file .tar)
done
```

### Step 3.3: Update Manifests for Private Registry

Create a script to update all image references in YAML files:

```bash
cat > scripts/update-image-references.sh << 'EOF'
#!/bin/bash
set -e

PRIVATE_REGISTRY="${1:?Usage: $0 <registry-host:port> [registry-namespace]}"
REGISTRY_NAMESPACE="${2:-knative}"

if [ -z "$PRIVATE_REGISTRY" ]; then
    echo "Error: Private registry address required"
    exit 1
fi

echo "Updating image references to ${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}"

# Create updated manifests directory
mkdir -p manifests-updated

# Process each YAML file
for yaml_file in manifests/*.yaml; do
    output_file="manifests-updated/$(basename $yaml_file)"
    
    # Read original image list and create sed replacements
    while IFS= read -r original_image; do
        if [[ "$original_image" == ko://* ]]; then
            # Handle ko:// references - these need to be mapped manually
            # Check the image-list.txt for actual image names
            component=$(echo "$original_image" | sed 's|ko://knative.dev/serving/cmd/||')
            # Map to known image names
            case "$component" in
                "activator")
                    sed_image="gcr.io/knative-releases/knative.dev/serving/cmd/activator"
                    ;;
                "autoscaler")
                    sed_image="gcr.io/knative-releases/knative.dev/serving/cmd/autoscaler"
                    ;;
                "controller")
                    sed_image="gcr.io/knative-releases/knative.dev/serving/cmd/controller"
                    ;;
                "webhook")
                    sed_image="gcr.io/knative-releases/knative.dev/serving/cmd/webhook"
                    ;;
                "queue")
                    sed_image="gcr.io/knative-releases/knative.dev/serving/cmd/queue"
                    ;;
                "default-domain")
                    sed_image="gcr.io/knative-releases/knative.dev/serving/cmd/default-domain"
                    ;;
                *)
                    sed_image="$original_image"
                    ;;
            esac
        else
            sed_image="$original_image"
        fi
        
        # Extract image name
        image_name=$(echo "$sed_image" | sed 's|.*/||' | cut -d: -f1 | cut -d@ -f1)
        new_image="${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/${image_name}"
        
        # Create sed command to replace
        # This is a simplified version - you may need to adjust based on your image naming
        sed -i.bak "s|${original_image}|${new_image}|g" "$yaml_file" 2>/dev/null || true
        sed -i.bak "s|${sed_image}|${new_image}|g" "$yaml_file" 2>/dev/null || true
    done < images/image-list.txt
    
    # Also replace common patterns
    sed -i.bak "s|gcr.io/knative-releases|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}|g" "$yaml_file"
    sed -i.bak "s|ko://knative.dev/serving/cmd/activator|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/activator|g" "$yaml_file"
    sed -i.bak "s|ko://knative.dev/serving/cmd/autoscaler|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/autoscaler|g" "$yaml_file"
    sed -i.bak "s|ko://knative.dev/serving/cmd/controller|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/controller|g" "$yaml_file"
    sed -i.bak "s|ko://knative.dev/serving/cmd/webhook|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/webhook|g" "$yaml_file"
    sed -i.bak "s|ko://knative.dev/serving/cmd/queue|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/queue|g" "$yaml_file"
    sed -i.bak "s|ko://knative.dev/serving/cmd/default-domain|${PRIVATE_REGISTRY}/${REGISTRY_NAMESPACE}/default-domain|g" "$yaml_file"
    
    # Copy to updated directory
    cp "$yaml_file" "$output_file"
    # Restore original (remove .bak)
    mv "${yaml_file}.bak" "$yaml_file" 2>/dev/null || true
    
    echo "Updated: $output_file"
done

echo "All manifests updated in manifests-updated/"
EOF

chmod +x scripts/update-image-references.sh

# Run the script
./scripts/update-image-references.sh <your-registry-host:port> knative
```

**Important**: After running this script, **manually verify** a few key files to ensure image references are correct:

```bash
# Check a deployment file
grep -A 2 "image:" manifests-updated/serving-core.yaml | head -20

# Verify queue-sidecar-image in configmap
grep "queue-sidecar-image" manifests-updated/serving-core.yaml
```

### Step 3.4: Install Knative Serving CRDs

Apply the Custom Resource Definitions first:

```bash
kubectl apply -f manifests-updated/serving-crds.yaml
```

Wait for CRDs to be established:

```bash
kubectl wait --for=condition=Established --all crd
```

### Step 3.5: Install Knative Serving Core

Install the core components:

```bash
kubectl apply -f manifests-updated/serving-core.yaml
```

### Step 3.6: Install Networking Layer

Install your chosen networking layer. For Kourier (simplest):

```bash
# Update kourier.yaml image references first, then:
kubectl apply -f manifests-updated/kourier.yaml

# Configure Knative to use Kourier
kubectl patch configmap/config-network \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
```

For Istio:

```bash
# Install Istio first (follow Istio airgap installation)
# Then install net-istio
kubectl apply -f manifests-updated/net-istio.yaml
```

### Step 3.7: Install Post-Installation Jobs

```bash
kubectl apply -f manifests-updated/serving-post-install-jobs.yaml
```

### Step 3.8: Install Default Domain Configuration

```bash
kubectl apply -f manifests-updated/serving-default-domain.yaml
```

## Phase 4: Post-Installation Configuration

After installation, configure Knative Serving for your environment.

### Step 4.1: Configure Default Domain

In airgapped environments, you'll need to configure DNS. Update the default domain:

```bash
# Option 1: Use a custom domain
kubectl patch configmap/config-domain \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"your-domain.com":""}}'

# Option 2: Use nip.io or sslip.io for testing (if DNS resolution works)
# This is already configured in default-domain job
```

### Step 4.2: Configure Image Pull Secrets (If Required)

If your private registry requires authentication:

```bash
# Create secret
kubectl create secret docker-registry registry-credentials \
  --docker-server=<your-registry-host:port> \
  --docker-username=<username> \
  --docker-password=<password> \
  --docker-email=<email> \
  --namespace knative-serving

# Patch service accounts to use the secret
kubectl patch serviceaccount controller \
  --namespace knative-serving \
  --type json \
  --patch '[{"op":"add","path":"/imagePullSecrets","value":[{"name":"registry-credentials"}]}]'

kubectl patch serviceaccount activator \
  --namespace knative-serving \
  --type json \
  --patch '[{"op":"add","path":"/imagePullSecrets","value":[{"name":"registry-credentials"}]}]'
```

### Step 4.3: Configure Registry for User Workloads

Configure Knative to allow pulling images from your private registry:

```bash
kubectl patch configmap/config-deployment \
  --namespace knative-serving \
  --type merge \
  --patch '{"data":{"registries-skipping-tag-resolving":"<your-registry-host:port>,kind.local,ko.local,dev.local"}}'
```

## Verification and Troubleshooting

### Verify Installation

Check that all pods are running:

```bash
# Check core components
kubectl get pods -n knative-serving

# Expected output should show all pods in Running or Completed state:
# NAME                                READY   STATUS      RESTARTS   AGE
# activator-xxx                       1/1     Running     0          2m
# autoscaler-xxx                      1/1     Running     0          2m
# controller-xxx                      1/1     Running     0          2m
# webhook-xxx                          1/1     Running     0          2m
# default-domain-xxx                   0/1     Completed   0          1m
```

Check CRDs are installed:

```bash
kubectl get crd | grep knative.dev

# Should show:
# configurations.serving.knative.dev
# revisions.serving.knative.dev
# routes.serving.knative.dev
# services.serving.knative.dev
# (and more...)
```

### Test with a Sample Application

Deploy a simple test service:

```bash
cat > test-service.yaml << 'EOF'
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: hello-world
  namespace: default
spec:
  template:
    spec:
      containers:
      - image: <your-registry>/knative/helloworld:latest
        # Or use a simple test image from your registry
        env:
        - name: TARGET
          value: "Knative Airgap"
EOF

# Update with an image from your registry, then:
kubectl apply -f test-service.yaml

# Check service status
kubectl get ksvc hello-world

# Get the service URL
kubectl get ksvc hello-world -o jsonpath='{.status.url}'
```

### Common Issues and Solutions

#### Issue: Pods stuck in ImagePullBackOff

**Symptoms**: Pods show `ImagePullBackOff` or `ErrImagePull` status

**Solutions**:
1. Verify image exists in registry:
   ```bash
   docker pull <your-registry>/knative/activator:<tag>
   ```

2. Check image pull secrets are configured correctly

3. Verify image references in manifests match what's in registry

4. Check registry accessibility from cluster nodes:
   ```bash
   # On a cluster node
   curl -k https://<your-registry>/v2/
   ```

#### Issue: Webhook not responding

**Symptoms**: Cannot create or update Knative resources, webhook errors in logs

**Solutions**:
1. Check webhook pod is running:
   ```bash
   kubectl get pods -n knative-serving -l app=webhook
   ```

2. Check webhook service:
   ```bash
   kubectl get svc -n knative-serving webhook
   ```

3. Verify webhook certificate (may need to check cert-manager if used)

4. Check webhook logs:
   ```bash
   kubectl logs -n knative-serving -l app=webhook
   ```

#### Issue: Services scale to zero but don't scale up

**Symptoms**: Services scale down but activator doesn't wake them up

**Solutions**:
1. Verify activator is running:
   ```bash
   kubectl get pods -n knative-serving -l app=activator
   ```

2. Check networking layer is properly configured

3. Verify autoscaler is running:
   ```bash
   kubectl get pods -n knative-serving -l app=autoscaler
   ```

4. Check autoscaler configuration:
   ```bash
   kubectl get configmap config-autoscaler -n knative-serving -o yaml
   ```

#### Issue: Cannot access services from outside cluster

**Symptoms**: Services deploy but are not accessible externally

**Solutions**:
1. Verify networking layer is installed and running:
   ```bash
   # For Kourier
   kubectl get pods -n kourier-system
   
   # For Istio
   kubectl get pods -n istio-system
   ```

2. Check ingress gateway service:
   ```bash
   kubectl get svc -n kourier-system kourier
   # or
   kubectl get svc -n istio-system istio-ingressgateway
   ```

3. Verify DNS configuration points to ingress gateway

4. Check network policies aren't blocking traffic

### Useful Debugging Commands

```bash
# View all Knative Serving resources
kubectl get all -n knative-serving

# Check controller logs
kubectl logs -n knative-serving -l app=controller --tail=100

# Check events for errors
kubectl get events -n knative-serving --sort-by='.lastTimestamp'

# Describe a problematic pod
kubectl describe pod <pod-name> -n knative-serving

# Check resource status
kubectl get ksvc,configuration,revision,route --all-namespaces

# Verify image pull policy
kubectl get deployment activator -n knative-serving -o yaml | grep imagePullPolicy
```

## Additional Considerations

### Security Hardening

1. **Image Scanning**: Scan all container images for vulnerabilities before loading into registry
2. **Network Policies**: Implement Kubernetes network policies to restrict traffic
3. **RBAC**: Review and tighten RBAC permissions as needed
4. **Secrets Management**: Use proper secrets management (e.g., HashiCorp Vault) for registry credentials
5. **Image Signing**: Consider implementing image signing and verification

### Performance Optimization

1. **Resource Limits**: Adjust resource requests/limits based on your workload
2. **Replica Counts**: Configure appropriate replica counts for HA
3. **Autoscaling**: Tune autoscaler settings for your use case
4. **Caching**: Enable image caching in your registry

### Monitoring and Observability

Set up monitoring for your airgapped environment:

1. **Metrics**: Deploy Prometheus to collect metrics
2. **Logging**: Centralized logging solution (e.g., ELK stack)
3. **Tracing**: Distributed tracing if needed
4. **Alerts**: Configure alerts for critical components

### Backup and Recovery

1. **Backup Manifests**: Keep copies of all installation manifests
2. **Registry Backup**: Regular backups of your container registry
3. **Configuration Backup**: Backup ConfigMaps and Secrets
4. **Disaster Recovery Plan**: Document recovery procedures

### Upgrading in Airgapped Environment

When upgrading Knative Serving:

1. Follow the same process: download new version on internet-connected machine
2. Review changelog for breaking changes
3. Test upgrade in non-production environment first
4. Backup current installation
5. Follow upgrade procedures from Knative documentation
6. Verify all components after upgrade

### Support and Resources

- **Knative Documentation**: [knative.dev/docs](https://knative.dev/docs)
- **Knative Slack**: Join #serving channel
- **GitHub Issues**: Report issues at [github.com/knative/serving](https://github.com/knative/serving)
- **Community**: Engage with Knative community for airgap-specific questions

## Conclusion

Installing Knative Serving in an airgapped environment requires careful planning and execution, but following this guide should help you achieve a successful deployment. The key steps are:

1. **Thorough preparation** - Download all required artifacts
2. **Secure transfer** - Move files safely to airgapped environment  
3. **Proper configuration** - Update manifests for your private registry
4. **Careful installation** - Install components in correct order
5. **Verification** - Ensure everything works correctly

Remember to test thoroughly in a non-production environment first, and keep detailed records of your installation process for future reference and troubleshooting.

Good luck with your airgapped Knative Serving installation!
