#!/usr/bin/env python

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

"""Simple redirection handler to Gubernator's GitHub service."""

import webapp2

class GitHubRedirect(webapp2.RequestHandler):
  def get(self):
    self.redirect("https://github-dot-knative-tests.appspot.com" + self.request.path_qs)

app = webapp2.WSGIApplication([(r'/.*', GitHubRedirect),], debug=True, config={})
