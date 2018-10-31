# Copyright 2018 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

SRC := test-infra/gubernator

deploy:
	# Fetch latest source, patch for our instance
	rm -fr test-infra
	git clone http://github.com/kubernetes/test-infra.git
	cp config.yaml $(SRC)
	cp redir_github.py $(SRC)
	sed -i -e '/^runtime: .*/a service: gubernator' $(SRC)/app.yaml
	sed -i -e "/^handlers:/a\- url: /timeline\n  script: redir_github.app\n" $(SRC)/app.yaml
	sed -i -e 's/user:kubernetes/user:knative/' $(SRC)/view_pr.py
	sed -i -e 's/Kubernetes/Knative/' $(SRC)/templates/index.html
	sed -i -e 's/k8s-testgrid.appspot.com/testgrid.knative.dev/' $(SRC)/filters.py
	sed -i -e 's/k8s-testgrid/knative-testgrid/' $(SRC)/testgrid.py
	# Deploy
	make -C ../prow get-cluster-credentials
	PROJECT=knative-tests make -C $(SRC) deploy
	# Cleanup
	rm -fr test-infra
