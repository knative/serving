import os
import pytz
import pickle
import numpy as np
import math
import pandas as pd
from datetime import datetime, timedelta
import matplotlib.pyplot as plt

from helper import hash_to_app, app_to_exec, application_names_to_mem

def get_per_timestep_range(start_time, end_time, date_format, timestep):
    if type(start_time) is str: 
        start_time_object = datetime.strptime(start_time, date_format)
        end_time_object = datetime.strptime(end_time, date_format)
    else:
        start_time_object = start_time
        end_time_object = end_time

    timestamps = []
    current_time = start_time_object
    while current_time <= end_time_object:
        timestamps.append(current_time.replace(microsecond=0))
        current_time += timedelta(seconds=timestep)
    timestamps.append(current_time.replace(microsecond=0))
    return timestamps


def get_per_timestep_request_frequency(incoming_requests_per_second_timestamps, date_format, all_ts, cs_or_ws, request_tag_list):
    timestamp_freq_map = {}
    timestamp_cs_map = {}
    request_tag_map = {}

    for ts, cw in zip(all_ts, cs_or_ws):
        if type(ts) == str:
            ts_obj = datetime.strptime(ts, date_format)
        else:
            ts_obj = ts
        for i in range(len(incoming_requests_per_second_timestamps)-1):
            if ts_obj >= incoming_requests_per_second_timestamps[i] and ts_obj < incoming_requests_per_second_timestamps[i+1]:
                if incoming_requests_per_second_timestamps[i] in timestamp_freq_map:
                    timestamp_freq_map[incoming_requests_per_second_timestamps[i]] += 1
                else:
                    timestamp_freq_map[incoming_requests_per_second_timestamps[i]] = 1
                if cw == 'c':
                    if incoming_requests_per_second_timestamps[i] in timestamp_cs_map:
                        timestamp_cs_map[incoming_requests_per_second_timestamps[i]] += 1
                    else:
                        timestamp_cs_map[incoming_requests_per_second_timestamps[i]] = 1
        # include 0s for all other times where there are no invocations
        for i in range(len(incoming_requests_per_second_timestamps)-1):
            if incoming_requests_per_second_timestamps[i] not in timestamp_freq_map:
                timestamp_freq_map[incoming_requests_per_second_timestamps[i]] = 0
            if incoming_requests_per_second_timestamps[i] not in timestamp_cs_map:
                timestamp_cs_map[incoming_requests_per_second_timestamps[i]] = 0
    for ts, request_tag in zip(all_ts, request_tag_list):
        if type(ts) == str:
            ts_obj = datetime.strptime(ts, date_format)
        else:
            ts_obj = ts
        
        for i in range(len(incoming_requests_per_second_timestamps)-1):
            if ts_obj >= incoming_requests_per_second_timestamps[i] and ts_obj < incoming_requests_per_second_timestamps[i+1]:
                # cold start tag times and frequencies
                if request_tag == "FAILED":
                    if incoming_requests_per_second_timestamps[i] in request_tag_map:
                        request_tag_map[incoming_requests_per_second_timestamps[i]] += 1
                    else:
                        request_tag_map[incoming_requests_per_second_timestamps[i]] = 1
        
        # include 0s for all other times where there are no invocations
        for i in range(len(incoming_requests_per_second_timestamps)-1):
            if incoming_requests_per_second_timestamps[i] not in request_tag_map:
                request_tag_map[incoming_requests_per_second_timestamps[i]] = 0
                
    return timestamp_freq_map, timestamp_cs_map, request_tag_map


def get_request_end_times(start_time, response_duration, exec_duration, date_format):
    if type(start_time) == str:
        start_time_obj = datetime.strptime(start_time, date_format)
    else:
        start_time_obj = start_time
    # start_time_obj = datetime.strptime(start_time, date_format)
    delta = timedelta(milliseconds=float(response_duration)-exec_duration)
    new_timestamp = start_time_obj + delta
    return new_timestamp.strftime("%Y-%m-%d %H:%M:%S.%f")


def get_cold_or_warm(response_duration, exec_duration, cold_start_threshold):
    # print("response_duration ", float(response_duration) - exec_duration, cold_start_threshold)
    return 'c' if float(response_duration) - exec_duration >= cold_start_threshold else 'w'


def get_request_concurrency(df, per_second_timestamps, date_format, timestep):
    # concurrency calculated on the basis of how many requests are active right now and 
    df["REQUEST_END_TIME"] = df.apply(lambda x: get_end_time(x.REQUEST_START_TIME, x.REQUEST_DURATION), axis=1)
    return get_ideal_average_concurrency(df)

def get_ideal_average_concurrency(df, type="request"):
    # counter to maintain the concurrency across timestamps
    concurrency = 0
    # all start times of invocations
    all_start_inv_times = []
    # all end times of invocations
    all_end_inv_times = []
    # [(st1, 's'), (st2, 's'), (et1, 'e'), ...]
    curr_inv_queue = []
    # this is going to be returned and contains arrival time to ideal average concurrency mappings
    map_at_to_ideal_avg_conc = {}

    # get all start and end times for invocations in 2 lists
    for _, row in df.iterrows():
        if type == "request":
            all_start_inv_times.append(row.REQUEST_START_TIME)
            all_end_inv_times.append(row.REQUEST_END_TIME)
        else:
            all_start_inv_times.append(row.POD_START_TIME)
            all_end_inv_times.append(row.POD_END_TIME)

    for start_times, end_times in zip(all_start_inv_times, all_end_inv_times):
        curr_inv_queue.append((start_times, "s"))
        curr_inv_queue.append((end_times, "e"))

    # sort the list so that all start and end times in the range are together and in cronological order
    curr_inv_queue.sort(key=lambda x: x[0])

    # keep popping the first elements so that we know wether to increase the concurrency of decrease it
    while len(curr_inv_queue) != 0:
        curr_elem = curr_inv_queue.pop(0)
        if curr_elem[1] == "s":
            concurrency += 1
        elif curr_elem[1] == "e":
            concurrency -= 1
        if curr_elem[0] not in map_at_to_ideal_avg_conc:
            map_at_to_ideal_avg_conc[curr_elem[0]] = (concurrency, curr_elem[1])
        elif map_at_to_ideal_avg_conc[curr_elem[0]][1] == 's' and curr_elem[1] == 'e':
            del map_at_to_ideal_avg_conc[curr_elem[0]]
        else:
            map_at_to_ideal_avg_conc[curr_elem[0]] = (concurrency, curr_elem[1])
        # print("curr timestamp ", curr_elem)
    map_at_to_ideal_avg_conc = dict(sorted(map_at_to_ideal_avg_conc.items(), key=lambda item: item[0]))
    return map_at_to_ideal_avg_conc


def get_end_time(request_start_time, request_duration):
    # TODO: why is the duration negative sometimes
    return request_start_time + timedelta(milliseconds=max(0, float(request_duration)))


def request_post_processing(data_folder, req_data_file, timestep, cutoff_time):
    """
    - number of requests per second at time t
    - number of cold starts at time t (no of cold starts is number of requests have response time above cold start threshold)
    - number of executing requests at time t (the number of warm requests executing at time t, cold requests will start executing at time t + response time - exec time)
    """
    df = pd.read_pickle(data_folder + req_data_file)
    df = df.sort_values(by="REQUEST_START_TIME")
    df.dropna(subset=["REQUEST_START_TIME"], inplace=True)

    # # remove all rows where the request start time is > 04-06 18
    # df = df[df["REQUEST_START_TIME"] < datetime.strptime("2024-04-06 18:00:00.000", "%Y-%m-%d %H:%M:%S.%f")]
    # drop all rows which have REQUEST_START_TIME < first request time + 30 minutes and > last request time - 30 minutes
    df = df[(df["REQUEST_START_TIME"] > df["REQUEST_START_TIME"].iloc[0] + timedelta(minutes=cutoff_time)) & (df["REQUEST_START_TIME"] < df["REQUEST_START_TIME"].iloc[-1] - timedelta(minutes=25*cutoff_time))]

    # for exec time we remove NaN from start time and duration only to be able to calculate concurrency
    no_st_and_dur_df = df.dropna(subset=["REQUEST_START_TIME", "REQUEST_DURATION"])

    # 2. calculate number of cold and warm starts per second
    COLD_START_THRESHOLD = 700 # ms

    incoming_requests_per_second_timestamps_freq = {}

    date_format = "%Y-%m-%d %H:%M:%S.%f"

    # get application to exec mapping
    app_to_exec_map = hash_to_app(df["APP_NAME"].values.tolist())
    df["COLD_OR_WARM"] = df.apply(lambda row: get_cold_or_warm(row.REQUEST_DURATION, app_to_exec_map[row.APP_NAME], COLD_START_THRESHOLD), axis=1)
    df.to_pickle(data_folder + "req_df_with_cold_or_warm.pickle")
    # no_st_and_dur_df["COLD_OR_WARM"] = no_st_and_dur_df.apply(lambda row: get_cold_or_warm(row.REQUEST_DURATION, app_to_exec_map[row.APP_NAME], COLD_START_THRESHOLD), axis=1)
    print("finished cold or warm")
    # 1. calculate incoming requests per second
    # get start and end time of incoming invocations
    first_request_start_time = df["REQUEST_START_TIME"].iloc[0]
    last_request_start_time = df["REQUEST_START_TIME"].iloc[-1]

    # get request timestamps rounded to seconds
    incoming_requests_per_second_timestamps = get_per_timestep_range(first_request_start_time, last_request_start_time, date_format, timestep)
    print("got request per sec")
    # get frequency of occurence of requests per second
    incoming_requests_per_second_timestamps_freq, timestamp_cs_map, failed_request_map = get_per_timestep_request_frequency(incoming_requests_per_second_timestamps, date_format, df["REQUEST_START_TIME"].astype('object').values.tolist(), df["COLD_OR_WARM"].values.tolist(), df["REQUEST_RESPONSE_TYPE"].values.tolist())
    request_concurrency_per_second_map = get_request_concurrency(no_st_and_dur_df, incoming_requests_per_second_timestamps, date_format, timestep)
    print("finished request concurrency")
    return incoming_requests_per_second_timestamps_freq, request_concurrency_per_second_map, timestamp_cs_map, failed_request_map 

def update_end_time(row, max_end_time):
    # max_end_time = datetime.strptime("2024-04-06 18:00:00.000", "%Y-%m-%d %H:%M:%S.%f")
    if not pd.isnull(row['POD_START_TIME']) and pd.isnull(row['POD_END_TIME']):
        row['POD_END_TIME'] = max_end_time
        row['POD_DURATION'] = int((max_end_time - row['POD_START_TIME']).total_seconds())
    return row


def get_ready_pod_count(df, app_name, app_to_mem_map, last_request_timestamp, cutoff_time):
    """
    ready pod count on the basis of alive pods responding to requests
        - pod start and end time through a dataframe
        - return: pod count and per second timestamp trace
    """
    df = df[df["POD_NAME"].str.contains(app_name)]
    print(df, df.columns, df["POD_START_TIME"])
    df = df.apply(lambda row: update_end_time(row, last_request_timestamp - timedelta(minutes=25*cutoff_time)), axis=1)
    curr_rpc_queue = []

    date_format = "%Y-%m-%d %H:%M:%S.%f"
    print(app_name, df, df.columns, df["POD_START_TIME"])
    pod_start_time = df["POD_START_TIME"].iat[0]

    for _, row in df.iterrows():
        curr_rpc_queue.append((row["POD_START_TIME"], 's', row.POD_NAME))
        curr_rpc_queue.append((row["POD_END_TIME"], 'e', row.POD_NAME))
    
    curr_rpc_queue.sort(key=lambda x: x[0])

    if type(pod_start_time) is str: 
        start_time_object = datetime.strptime(pod_start_time, date_format)
    else:
        start_time_object = pod_start_time
        
    start_time_object -= timedelta(seconds=1)

    rpc = 0
    rpc_list = []
    rpc_timestamp_list = []
    mem_list = []
    mem_alloc = 0

    rpc_list.append(0)
    mem_list.append(0)
    rpc_timestamp_list.append(start_time_object)
    
    while len(curr_rpc_queue) != 0:
        curr_elem = curr_rpc_queue.pop(0)
        if curr_elem[1] == 's':
            rpc += 1
            mem_alloc += app_to_mem_map[curr_elem[2][:20]]
        elif curr_elem[1] == 'e':
            rpc -= 1
            mem_alloc -= app_to_mem_map[curr_elem[2][:20]]
        # if timestamp already exists then for rpc replace it at previous index and for mem_list also replace it
        if curr_elem[0] in rpc_timestamp_list:
            index = rpc_timestamp_list.index(curr_elem[0])
            rpc_list[index] = rpc
            mem_list[index] = mem_alloc
        else:
            rpc_list.append(rpc)
            mem_list.append(mem_alloc)
            rpc_timestamp_list.append(curr_elem[0])
    return rpc_list, rpc_timestamp_list, mem_list

# TODO: make this generic, there are 3 places where the same code is being used
def get_execution_utilization(app_name, memory_allocated_map, exec_start_time_list, exec_end_time_list, date_str):
    curr_exec_queue = []
    date_format = "%Y-%m-%d %H:%M:%S.%f"

    for exec_start_time, exec_end_time in zip(exec_start_time_list, exec_end_time_list):
        # TODO: need to do the same in case we parse an object instead of a string
        if type(exec_end_time) is str:
            if len(exec_start_time.split(".")[-1]) == 3:
                exec_start_time_object = datetime.strptime(exec_start_time+"000", date_format)
            else:
                exec_start_time_object = datetime.strptime(exec_start_time[:-3], date_format)

            if len(exec_end_time.split(".")[-1]) <= 3:
                exec_end_time_object = datetime.strptime(exec_end_time+"000", date_format)
            else:
                exec_end_time_object = datetime.strptime(exec_end_time[:-3], date_format)

            curr_exec_queue.append((exec_start_time_object, 's', app_name))
            curr_exec_queue.append((exec_end_time_object, 'e', app_name))
    curr_exec_queue.sort(key=lambda x: x[0])
    executing_request_count = 0
    executing_request_timestamp = []
    execution_utilization_list = []
    execution_utilization_sum = 0

    while len(curr_exec_queue) != 0:
        curr_elem = curr_exec_queue.pop(0)
        if curr_elem[1] == 's':
            executing_request_count += 1
            execution_utilization_sum += memory_allocated_map[curr_elem[2]]
        elif curr_elem[1] == 'e':
            executing_request_count -= 1
            execution_utilization_sum -= memory_allocated_map[curr_elem[2]]
        execution_utilization_list.append(execution_utilization_sum)
        executing_request_timestamp.append(curr_elem[0])
    return execution_utilization_list, executing_request_timestamp

def get_area_under_the_curve(data_points, data_timestamps, first_timestamp, last_timestamp, cutoff_time, app_name = None):
    total_metric = 0
    prev_timestamp = data_timestamps[0]
    prev_datapoint = data_points[0]
    for data_point, data_timestamp in zip(data_points[1:], data_timestamps[1:]):
        total_metric += max(0, (min((last_timestamp - timedelta(minutes=25*cutoff_time)), max(data_timestamp, first_timestamp + timedelta(minutes=cutoff_time))) - min(last_timestamp - timedelta(minutes=25*cutoff_time), max(prev_timestamp, first_timestamp + timedelta(minutes=cutoff_time)))).total_seconds()) * prev_datapoint
        prev_datapoint = data_point 
        prev_timestamp = data_timestamp
    return total_metric

def get_per_min_buckets(map_at_to_ideal_avg_conc):
    per_min_buckets = {}
    for ts, conc_type in map_at_to_ideal_avg_conc.items():
        min_ts = ts.replace(second=0, microsecond=0)
        # print(f"{min_ts} {conc_type}")
        if min_ts in per_min_buckets:
            if conc_type[1] == 's':
                per_min_buckets[min_ts] += 1
        else:
            if conc_type[1] == 's':
                per_min_buckets[min_ts] = 1
    return per_min_buckets

def get_avg_req_conc(concurrency_map):

    start_time = list(concurrency_map.keys())[0]
    end_time = list(concurrency_map.keys())[-1]

    per_min_buckets = [0]*int((end_time-start_time).total_seconds() / 60.0)

    start_time = list(concurrency_map.keys())[0]

    prev_bucket_conc = 0

    # get average request concurrency per min
    for i in range(len(per_min_buckets)-1):
        cur_bucket_events = []
        cur_bucket_ts = []
        cur_bucket_conc = 0
        avg_conc = 0

        for ts, event in concurrency_map.items():
            if ts > start_time+timedelta(minutes=i+1):
                break
            if ts >= start_time+timedelta(minutes=i) and ts < start_time+timedelta(minutes=i+1):
                cur_bucket_events.append(event)
                cur_bucket_ts.append(ts)

        if len(cur_bucket_ts) != 0:
            if cur_bucket_ts[0] != start_time+timedelta(minutes=i):
                cur_bucket_events.insert(0, (prev_bucket_conc, 's'))
                cur_bucket_ts.insert(0, start_time+timedelta(minutes=i))

            for j in range(len(cur_bucket_ts)-1):
                cur_event = cur_bucket_events[j]
                duration = (cur_bucket_ts[j+1] - cur_bucket_ts[j]).total_seconds()
                
                cur_bucket_conc += cur_event[0] * duration

                prev_bucket_conc = cur_event[0]
            
            if cur_bucket_ts[-1] != i+1:
                cur_bucket_conc += cur_bucket_events[-1][0] * (start_time+timedelta(minutes=i+1) - cur_bucket_ts[-1]).total_seconds()
                prev_bucket_conc = cur_bucket_events[-1][0]

        if cur_bucket_conc == 0:
            cur_bucket_conc = prev_bucket_conc
            avg_conc = cur_bucket_conc
        else:
            avg_conc = cur_bucket_conc / 60.0
        print(f"Bucket {i}: {avg_conc}")
        per_min_buckets[i] = avg_conc
    
    return per_min_buckets

def plot(data_folder, req_data_file, pod_data_file, plot_folder, output_folder, timestep, cutoff_time):
    print("inside plot")
    incoming_requests_per_second_timestamps_freq, request_concurrency_per_second_map, timestamp_cs_map, failed_request_map = request_post_processing(data_folder, req_data_file, timestep, cutoff_time)

    if not os.path.exists(plot_folder):
        print("creating directory structure...")
        os.makedirs(plot_folder)
    
    fig, ax = plt.subplots(7, 1, figsize=(10, 10))
    fig.suptitle("Request & Pod Behaviour Plots (100 mid apps)")
    ax[0].stem([ts for ts in incoming_requests_per_second_timestamps_freq.keys()], list(incoming_requests_per_second_timestamps_freq.values()), markerfmt="blue")
    ax[0].set_xlabel("Time")
    ax[0].set_ylabel("# RPM")

    first_request_timestamp = list(incoming_requests_per_second_timestamps_freq.keys())[0]
    last_request_timestamp = list(incoming_requests_per_second_timestamps_freq.keys())[-1]
    
    df = pd.read_pickle(data_folder + pod_data_file)

    app_names = [pod_name[:20] for pod_name in df["POD_NAME"].values.tolist()]
    unique_app_names = []
    for app in app_names:
        if app not in unique_app_names:
            unique_app_names.append(app)
    
    df_per_app = pd.DataFrame()
    for app_name in unique_app_names:
        rpc_list, rpc_timestamp_list, mem_list = get_ready_pod_count(df, app_name, application_names_to_mem, last_request_timestamp, cutoff_time)
        total_memory_allocated = get_area_under_the_curve(mem_list, rpc_timestamp_list, first_request_timestamp, last_request_timestamp, cutoff_time)
        row_df = pd.DataFrame({
            "APP_NAME": app_name,
            "MEM_ALLOC": [total_memory_allocated]
        })
        df_per_app = pd.concat([df_per_app, row_df], ignore_index=True)

    df = df.apply(lambda row: update_end_time(row, last_request_timestamp - timedelta(minutes=25*cutoff_time)), axis=1)
    df = df.sort_values(by="POD_START_TIME")
    rpc_to_ts_map = get_ideal_average_concurrency(df, type="pod")
    rps_start_ts = list(incoming_requests_per_second_timestamps_freq.keys())[0]
    rps_start_ts = pd.to_datetime(rps_start_ts)
    rpc_to_ts_map = {ts: rpc for ts, rpc in rpc_to_ts_map.items() if ts >= rps_start_ts}

    ax[1].step(pd.to_datetime(list(rpc_to_ts_map.keys())), [conc[0] for conc in rpc_to_ts_map.values()], where='post')

    ax[1].set_xlabel("Time")
    ax[1].set_ylabel("Pod Count")
    
    # ax[1].stem([ts for ts in timestamp_cs_map.keys()], list(timestamp_cs_map.values()), markerfmt="red")
    ax[2].plot([ts for ts in timestamp_cs_map.keys()], list(timestamp_cs_map.values()), color="red")
    ax[2].set_ylabel("#CSPM")
    ax[2].set_xlabel("Time")
    
    req_conc_start_time = list(request_concurrency_per_second_map.keys())[0]

    # avg_req_conc_per_min_buckets = get_avg_req_conc(request_concurrency_per_second_map)

    # with open('avg_req_conc_per_min_buckets.pkl', 'wb') as f:
    #     pickle.dump(avg_req_conc_per_min_buckets, f)

    # with open('avg_req_conc_per_min_buckets.pkl', 'rb') as f:
    #     avg_req_conc_per_min_buckets = pickle.load(f)

    # # plt.step(range(len(avg_req_conc_per_min_buckets)), avg_req_conc_per_min_buckets, where='post')
    # ax[3].plot([req_conc_start_time + timedelta(minutes=i) for i in range(len(avg_req_conc_per_min_buckets))], avg_req_conc_per_min_buckets, color='green')
    # # ax[2].plot(request_concurrency_per_second_map.keys(), [conc[0] for conc in request_concurrency_per_second_map.values()], color='green')
    # ax[3].set_ylabel("Avg Req Conc")
    # ax[3].set_xlabel("Time")

    # ax[3].stem([ts for ts in failed_request_map.keys()], list(failed_request_map.values()), markerfmt="black")
    ax[4].plot([ts for ts in failed_request_map.keys()], list(failed_request_map.values()), color="black")
    ax[4].set_ylabel("Failed RPM")
    ax[4].set_xlabel("Time")

    # RPC
    first_inv_time = list(incoming_requests_per_second_timestamps_freq.keys())[0]
    last_inv_time = list(request_concurrency_per_second_map.keys())[-1]

    # resource plots
    # queue_proxy end time for each request
    df = pd.read_pickle(data_folder + "qp_df.pickle")
    # remove all rows where the request start time is > 04-06 18
    
    df["IngressStartTime"] = pd.to_datetime(df["IngressStartTime"]).dt.tz_convert(None)
    # df = df[df["IngressStartTime"] < datetime.strptime("2024-04-06 18:00:00.000", "%Y-%m-%d %H:%M:%S.%f").replace(tzinfo=pytz.UTC)]
    # df = df[(df["IngressStartTime"] > first_request_timestamp + timedelta(minutes=cutoff_time)) & (df["IngressStartTime"] < last_request_timestamp - timedelta(minutes=5*cutoff_time))]
    # remove requests with no exec times
    df.dropna(inplace=True)
    # TODO: make this generic for different applications as well
    util_df = pd.DataFrame()
    for app_name in df["APP_NAME"].unique():
        # print(f"iteration for {app_name}")
        exec_util, exec_timestamp = get_execution_utilization(app_name, application_names_to_mem, df[df["APP_NAME"] == app_name]["QPStartTime"].values.tolist(), df[df["APP_NAME"] == app_name]["QPEndTime"].values.tolist(), str(rpc_timestamp_list[0]).split(' ')[0])
        total_memory_utilized = get_area_under_the_curve(exec_util, exec_timestamp, first_request_timestamp, last_request_timestamp, cutoff_time, app_name)
        row_df = pd.DataFrame({
            "APP_NAME": app_name,
            "MEM_UTIL": [total_memory_utilized]
        })
        util_df = pd.concat([util_df, row_df], ignore_index=True)
    print(df_per_app, df_per_app.columns, util_df, util_df.columns)
    df_per_app = pd.merge(df_per_app, util_df, on="APP_NAME")
    df_per_app.to_pickle("../output/alloc_util/femux_default_10_min_ka_per_app_alloc_util.pickle")
    # print(df_per_app, df_per_app["MEM_UTIL"].sum(), df_per_app["MEM_ALLOC"].sum())
    # ax[4].plot(df['COLLECTION_TIME'], df['MEM_UTIL'], color="purple", label='usage')
    # ax[4].step(exec_timestamp, exec_util, color="purple", label='usage', where='post')
    index = 0
    mem_to_ts_map = {}
    for mem, ts in zip(mem_list[index:], rpc_timestamp_list[index:]):
        if mem != np.nan and ts != np.nan:
            if not isinstance(ts, pd._libs.tslibs.nattype.NaTType):
                mem_to_ts_map[ts] = mem

    df = pd.read_pickle(output_folder + "node_resource_data.pickle")
    df["COLLECTION_TIME"] = df["COLLECTION_TIME"].apply(to_datetime)
    df = df[(df['COLLECTION_TIME'] >= first_inv_time + timedelta(minutes=cutoff_time)) & (df['COLLECTION_TIME'] <= last_inv_time - timedelta(minutes=25*cutoff_time))]
    # df = df[df["COLLECTION_TIME"] < datetime.strptime("2024-04-06 18:00:00.000", "%Y-%m-%d %H:%M:%S.%f")]
    ax[5].plot(df['COLLECTION_TIME'], df['MEM_PCT'], color="purple", label='wkr')
    ax[5].set_ylabel("node memory(%)")
    ax[5].set_xlabel("Time")

    ax[6].plot(df['COLLECTION_TIME'], df['CPU_PCT'], color="purple", label='wkr')
    ax[6].set_ylabel("node cpu(%)")
    ax[6].set_xlabel("Time")


    # get area under the curve for memory alloc and util
    total_memory_allocated = get_area_under_the_curve(mem_list, rpc_timestamp_list, first_request_timestamp, last_request_timestamp, cutoff_time)
    total_memory_utilized = get_area_under_the_curve(exec_util, exec_timestamp, first_request_timestamp, last_request_timestamp, cutoff_time)

    # aggregate metrics
    print(f"number of requests: {sum(list(incoming_requests_per_second_timestamps_freq.values()))}")
    print(f"number of cold starts: {sum(list(timestamp_cs_map.values()))}")
    print(f"number of dropped requests: {sum(list(failed_request_map.values()))}")
    print(f"total memory allocated: {df_per_app['MEM_ALLOC'].sum()}")
    print(f"total memory utilized: {df_per_app['MEM_UTIL'].sum()}")


    plt.legend()
    fig.tight_layout()
    plt.savefig(plot_folder + "request_behaviour_plot.pdf")


def to_datetime(int_time):
    dt = datetime.fromtimestamp(int_time)
    dt_str = dt.strftime('%Y-%m-%d %H:%M:%S')
    date_format = '%Y-%m-%d %H:%M:%S'

    # Convert the string to a datetime object
    date_object = datetime.strptime(dt_str, date_format)

    return date_object