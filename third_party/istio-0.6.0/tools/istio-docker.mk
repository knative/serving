## Copyright 2018 Istio Authors
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.

.PHONY: docker

docker: docker.all

$(ISTIO_DOCKER) $(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

# static files/directories that are copied from source tree

PROXY_JSON_FILES:=pilot/docker/envoy_pilot.json \
                  pilot/docker/envoy_pilot_auth.json \
                  pilot/docker/envoy_mixer.json \
                  pilot/docker/envoy_mixer_auth.json

NODE_AGENT_TEST_FILES:=security/docker/start_app.sh \
                       security/docker/app.js

GRAFANA_FILES:=addons/grafana/dashboards.yaml \
               addons/grafana/datasources.yaml \
               addons/grafana/grafana.ini

# note that "dashboards" is a directory rather than a file
$(ISTIO_DOCKER)/dashboards: addons/grafana/$$(notdir $$@) | $(ISTIO_DOCKER)
	cp -r $< $(@D)

# note that "js" and "force" are directories rather than a file
$(ISTIO_DOCKER)/js $(ISTIO_DOCKER)/force: addons/servicegraph/$$(notdir $$@) | $(ISTIO_DOCKER)
	cp -r $< $(@D)

# generated content
$(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key: ${GEN_CERT} | ${ISTIO_DOCKER}
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_DOCKER}/istio_ca.crt \
                    --out-priv=${ISTIO_DOCKER}/istio_ca.key --organization="k8s.cluster.local" \
                    --self-signed=true --ca=true
$(ISTIO_DOCKER)/node_agent.crt $(ISTIO_DOCKER)/node_agent.key: ${GEN_CERT} $(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_DOCKER}/node_agent.crt \
                    --out-priv=${ISTIO_DOCKER}/node_agent.key --organization="NodeAgent" \
                    --host="nodeagent.google.com" --signer-cert=${ISTIO_DOCKER}/istio_ca.crt \
                    --signer-priv=${ISTIO_DOCKER}/istio_ca.key

# directives to copy files to docker scratch directory

# tell make which files are copied form go/out
DOCKER_FILES_FROM_ISTIO_OUT:=pilot-test-client pilot-test-server pilot-test-eurekamirror \
                             pilot-discovery pilot-agent sidecar-injector servicegraph mixs \
                             istio_ca node_agent multicluster_ca
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_OUT), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT)/$(FILE) | $(ISTIO_DOCKER); cp $$< $$(@D)))

# This generates rules like:
#$(ISTIO_DOCKER)/pilot-agent: $(ISTIO_OUT)/pilot-agent | $(ISTIO_DOCKER)
# 	cp $$< $$(@D))

# tell make which files are copied from the source tree
DOCKER_FILES_FROM_SOURCE:=pilot/docker/prepare_proxy.sh docker/ca-certificates.tgz tools/deb/envoy_bootstrap_tmpl.json \
                          $(PROXY_JSON_FILES) $(NODE_AGENT_TEST_FILES) $(GRAFANA_FILES) \
                          pilot/docker/certs/cert.crt pilot/docker/certs/cert.key
$(foreach FILE,$(DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))

# pilot docker images

docker.app: $(ISTIO_DOCKER)/pilot-test-client $(ISTIO_DOCKER)/pilot-test-server \
            $(ISTIO_DOCKER)/cert.crt $(ISTIO_DOCKER)/cert.key
docker.eurekamirror: $(ISTIO_DOCKER)/pilot-test-eurekamirror
docker.pilot:        $(ISTIO_DOCKER)/pilot-discovery
docker.proxy docker.proxy_debug: $(ISTIO_DOCKER)/pilot-agent
$(foreach FILE,$(PROXY_JSON_FILES),$(eval docker.proxy docker.proxy_debug: $(ISTIO_DOCKER)/$(notdir $(FILE))))
docker.proxy docker.proxy_debug: $(ISTIO_DOCKER)/envoy_bootstrap_tmpl.json
docker.proxy_init: $(ISTIO_DOCKER)/prepare_proxy.sh
docker.sidecar_injector: $(ISTIO_DOCKER)/sidecar-injector

PILOT_DOCKER:=docker.app docker.eurekamirror docker.pilot docker.proxy \
              docker.proxy_debug docker.proxy_init docker.sidecar_injector
$(PILOT_DOCKER): pilot/docker/Dockerfile$$(suffix $$@) | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# addons docker images

SERVICEGRAPH_DOCKER:=docker.servicegraph docker.servicegraph_debug
$(SERVICEGRAPH_DOCKER): addons/servicegraph/docker/Dockerfile$$(suffix $$@) \
		$(ISTIO_DOCKER)/servicegraph $(ISTIO_DOCKER)/js $(ISTIO_DOCKER)/force | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# mixer docker images

MIXER_DOCKER:=docker.mixer docker.mixer_debug
$(MIXER_DOCKER): mixer/docker/Dockerfile$$(suffix $$@) \
		$(ISTIO_DOCKER)/ca-certificates.tgz $(ISTIO_DOCKER)/mixs | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# security docker images

docker.istio-ca:        $(ISTIO_DOCKER)/istio_ca     $(ISTIO_DOCKER)/ca-certificates.tgz
docker.istio-ca-test:   $(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key
docker.node-agent:      $(ISTIO_DOCKER)/node_agent
docker.node-agent-test: $(ISTIO_DOCKER)/node_agent $(ISTIO_DOCKER)/istio_ca.key \
                        $(ISTIO_DOCKER)/node_agent.crt $(ISTIO_DOCKER)/node_agent.key
docker.multicluster-ca: $(ISTIO_DOCKER)/multicluster_ca
$(foreach FILE,$(NODE_AGENT_TEST_FILES),$(eval docker.node-agent-test: $(ISTIO_DOCKER)/$(notdir $(FILE))))

SECURITY_DOCKER:=docker.istio-ca docker.istio-ca-test docker.node-agent docker.node-agent-test docker.multicluster-ca
$(SECURITY_DOCKER): security/docker/Dockerfile$$(suffix $$@) | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# grafana image

$(foreach FILE,$(GRAFANA_FILES),$(eval docker.grafana: $(ISTIO_DOCKER)/$(notdir $(FILE))))
docker.grafana: addons/grafana/Dockerfile$$(suffix $$@) $(GRAFANA_FILES) $(ISTIO_DOCKER)/dashboards
	$(DOCKER_RULE)

DOCKER_TARGETS:=$(PILOT_DOCKER) $(SERVICEGRAPH_DOCKER) $(MIXER_DOCKER) $(SECURITY_DOCKER) docker.grafana

DOCKER_RULE=time (cp $< $(ISTIO_DOCKER)/ && cd $(ISTIO_DOCKER) && \
            docker build -t $(HUB)/$(subst docker.,,$@):$(TAG) -f Dockerfile$(suffix $@) .)

docker.all: $(DOCKER_TARGETS)

# for each docker.XXX target create a tar.docker.XXX target that says how
# to make a $(ISTIO_OUT)/docker/XXX.tar.gz from the docker XXX image
# note that $(subst docker.,,$(TGT)) strips off the "docker." prefix, leaving just the XXX
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT) | $(ISTIO_DOCKER_TAR) ; \
   time (docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(TAG) && \
         gzip ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${ISTIO_OUT}/docker/
docker.save: $(DOCKER_TAR_TARGETS)

# if first part of URL (i.e., hostname) is gcr.io then use gcloud for push
$(if $(findstring gcr.io,$(firstword $(subst /, ,$(HUB)))),\
        $(eval DOCKER_PUSH_CMD:=gcloud docker -- push),$(eval DOCKER_PUSH_CMD:=docker push))

# for each docker.XXX target create a push.docker.XXX target that pushes
# the local docker image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(DOCKER_TARGETS),$(eval push.$(TGT): | $(TGT) ; \
        time ($(DOCKER_PUSH_CMD) $(HUB)/$(subst docker.,,$(TGT)):$(TAG))))

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_PUSH_TARGETS+=push.$(TGT)))

# This target pushes each docker image to specified HUB and TAG.
# The push scripts support a comma-separated list of HUB(s) and TAG(s),
# but I'm not sure this is worth the added complexity to support.

# Deprecated - just use docker, no need to retag.
docker.tag: docker

# Will build and push docker images.
docker.push: $(DOCKER_PUSH_TARGETS)
