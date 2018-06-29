#!/usr/bin/env python

# Copyright 2018 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import print_function

import csv
from matplotlib import pylab
import sys

name = sys.argv[1]
concurrency = int(name)

# bucketize concurrency changes in this interval in milliseconds
bucket_interval = 50

# converts nanoseconds into milliseconds
def nanosToMillis(n):
    return n / 1000000

# returns the timestamp on when a given scale was reached.
# -1 means this scale was never reached
def timeToScale(seq, scale):
    for result in seq:
        if result['concurrency'] >= scale:
            return result['time']
    return -1

# read data from the csv file
results = []
with open('data.csv') as csvfile:
    reader = csv.DictReader(csvfile, fieldnames=['start_time', 'end_time'])
    for row in reader:
        # emits 'events' for each start and end of a request
        results.append({'time': nanosToMillis(int(row['start_time'])), 'concurrency_modifier': 1})
        results.append({'time': nanosToMillis(int(row['end_time'])), 'concurrency_modifier': -1})

# sort the events by their time
sorted_results = sorted(results, key=lambda x: x['time'])

accumulated_results = []
start_time = sorted_results[0]['time']
current_time = -1
current_concurrency = 0

for result in sorted_results:
    result_time = result['time']
    concurrency_modifier = result['concurrency_modifier']

    current_concurrency = current_concurrency + concurrency_modifier

    # bucketize concurrency values to smoothen out the plot
    if current_time - bucket_interval <= result_time and current_time + bucket_interval >= result_time:
        accumulated_results[-1]['concurrency'] = current_concurrency
    else:
        # Makes the timestamps relative to the start time
        accumulated_results.append({'time': result_time - start_time, 'concurrency': current_concurrency})

    current_time = result_time

# Report raw values
print('Scaling statistics:')
print('Time to scale: ' + str(timeToScale(accumulated_results, concurrency)) + ' ms')
print('Time to scale breakdown:')
for scale in range(1, concurrency + 1):
    print(str(scale) + ': ' + str(timeToScale(accumulated_results, scale)) + ' ms')

# Draw a plot
fig = pylab.figure(figsize=(10, 6))
ax = fig.add_subplot(1, 1, 1)

ax.plot([e['time'] for e in accumulated_results], [e['concurrency'] for e in accumulated_results], label='observed concurrency')

ax.set_xlabel('time (milliseconds)')
ax.set_ylabel('observed concurrency')
ax.grid(which='minor', alpha=0.2)
ax.grid(which='major', alpha=0.5)
ax.legend(loc='upper right')
fig.tight_layout()

pylab.savefig(name + '.png')