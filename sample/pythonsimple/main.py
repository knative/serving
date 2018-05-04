'''
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
'''

import os
import traceback

from flask import Flask

app = Flask(__name__)


@app.route('/')
def get():
  target = os.environ.get('TARGET') or 'NOT SPECIFIED'
  return 'Hello World from Python: %s!\n' % target

@app.route('/error')
def getError():
  try:
    print(undefined)
  except Exception:
    with open('/var/log/error.log', 'a') as f:
      traceback.print_exc(file=f)
    traceback.print_exc()
    return 'exception stack trace logs were send to stderr and /var/log/error.log\n'

if __name__ == '__main__':
  app.run(host='127.0.0.1', port=8080, debug=True)
