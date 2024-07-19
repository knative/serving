

import json
import yaml
import os
from datetime import timedelta

import pandas as pd
import matplotlib.pyplot as plt

def get_arrival_time(interarrival_times):
    arrival_times = []
    for iat_per_app in interarrival_times:
        cur_arrival_times = []
        curr_time = 0
        for iat in iat_per_app:
            curr_time += iat
            cur_arrival_times.append(curr_time)
        arrival_times.append(cur_arrival_times)
    return arrival_times
            

def get_min_buckets():
    iter = 0
    per_min_buckets = []

    while iter < TOTAL_EXPERIMENT_TIME:
        per_min_buckets.append(0)
        iter += 1
    
    return per_min_buckets


# Load the JSON file
with open('../data/misc/new_json_files/work_load_configs_rep_mid_meta_func_100_IAT.json', 'r') as json_file:
    data = json.load(json_file)

TOTAL_EXPERIMENT_TIME = 18*60 # min

# Extract interarrival times
interarrival_times =  [data['instances'][instance]['interarrivals_list'] for i, instance in enumerate(data['instances'])] # [data['instances'][f'instance_{index}']['interarrivals_list']]
arrival_times = get_arrival_time(interarrival_times)

per_min_buckets = get_min_buckets()
counter = 0
for i in range(len(per_min_buckets)-1):
    for per_app_arrival_times in arrival_times:

        for arrival_time in per_app_arrival_times:
            if arrival_time/60.0 >= i and arrival_time/60.0 < i+1:
                per_min_buckets[i] += 1
    print(f"Bucket {i} completed")

plt.stem(per_min_buckets)
plt.xlabel("Time (min)")
plt.ylabel("# of requests per min")
plt.savefig("combined_arrival_times.pdf")
print(sum(per_min_buckets[:TOTAL_EXPERIMENT_TIME]))

# def get_event_based_concurrency(df):
#     request_event_queue = []

#     for _, row in df.iterrows():
#         # use IAT to get AT
#         arrival_times = get_arrival_time(row.IAT)

#         # use AT and ExecDurations to get request concurrency
#         for arrival_time in arrival_times:
#             request_event_queue.append((arrival_time, 's'))
#             request_event_queue.append((arrival_time + (row.ExecDurations)/1000.0, 'e'))

#     request_event_queue.sort(key=lambda x: x[0])

#     request_concurrency = 0
#     concurrency_map = {}
#     for event in request_event_queue:
#         if event[1] == 's':
#             request_concurrency += 1
#         else:
#             request_concurrency -= 1

#         if event[0] not in concurrency_map:
#             concurrency_map[event[0]] = (request_concurrency, event[1])
#         elif concurrency_map[event[0]][1] == 's' and event[1] == 'e':
#             del concurrency_map[event[0]]
#         else:
#             concurrency_map[event[0]] = (request_concurrency, event[1])

#     return concurrency_map

# # use arrival times and request exec time per app in df to get the request concurrency
# df = pd.read_pickle('../data/mid_meta_func_deployment_100/dataframe/workload_event_data_max_72_100_apps.pickle')


# concurrency_map = get_event_based_concurrency(df)

# # print((concurrency_map))

# per_min_buckets = get_min_buckets()

# start_time = list(concurrency_map.keys())[0]

# prev_bucket_conc = 0

# # get average request concurrency per min
# for i in range(len(per_min_buckets)-1):
#     cur_bucket_events = []
#     cur_bucket_ts = []
#     cur_bucket_conc = 0
#     avg_conc = 0

#     for ts, event in concurrency_map.items():
#         # print(ts)
#         ts = ts/60.0

#         if ts > i+1:
#             break

#         if ts >= i and ts < i+1:
#             cur_bucket_events.append(event)
#             cur_bucket_ts.append(ts)

#     if len(cur_bucket_ts) != 0:
#         if cur_bucket_ts[0] != i:
#             cur_bucket_events.insert(0, (prev_bucket_conc, 's'))
#             cur_bucket_ts.insert(0, i)

#         for j in range(len(cur_bucket_ts)-1):
#             cur_event = cur_bucket_events[j]
#             duration = cur_bucket_ts[j+1] - cur_bucket_ts[j]
            
#             cur_bucket_conc += cur_event[0] * duration

#             prev_bucket_conc = cur_event[0]
        
#         if cur_bucket_ts[-1] != i+1:
#             cur_bucket_conc += cur_bucket_events[-1][0] * (i+1 - cur_bucket_ts[-1])
#             prev_bucket_conc = cur_bucket_events[-1][0]

#     print(f"Bucket {i}: {cur_bucket_conc}")
#     if cur_bucket_conc == 0:
#         cur_bucket_conc = prev_bucket_conc
#     avg_conc = cur_bucket_conc
#     avg_conc = cur_bucket_conc
#     per_min_buckets[i] = avg_conc


# # print(concurrency_map)

# # plt.step(concurrency_map.keys(), [val[0] for val in concurrency_map.values()], where='post')
# plt.step(range(len(per_min_buckets)), per_min_buckets, where='post')
# plt.savefig("request_concurrency.pdf")
