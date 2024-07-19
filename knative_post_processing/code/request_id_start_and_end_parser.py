import re
import numpy as np
import pandas as pd
from datetime import datetime, timedelta
import matplotlib.pyplot as plt


# we are going to use pandas and string regex ops to extract data

def get_request_id_start_and_end_data(DATA_FOLDER, OUTPUT_FOLDER):
    DATA_FILE = "pod_name_to_request_id_mapping.csv"

    input_df = pd.read_csv(DATA_FOLDER + DATA_FILE)

    # request_id: [start, end]
    request_id_to_ingress_start_map = {}
    request_id_to_queue_proxy_end_map = {}
    request_id_to_response_type_map = {}
    request_id_to_pod_map = {}
    request_id_to_app_map = {}
    output_df = pd.DataFrame()

    # for each row extract the log message that needs to be parsed
    for _, row in input_df.iterrows():
        # get request_id to pod and node mapping from message data for the application pod
        log_string_row = row["message"]
        pod_name_row = row["knative_dev/pod"]
        node_name_row = row["kubernetes.host"]
        pod_name_pattern = r'\b[a-zA-Z0-9-]+-deployment-[a-f0-9]+-[a-z0-9]+\b'
        # float because elasticsearch populates nan (float) instead of None or NaN
        if not isinstance(pod_name_row, float):
            pod_name_match = re.search(pod_name_pattern, pod_name_row)
            if pod_name_match and not isinstance(log_string_row, float) and "[queue-proxy]" not in log_string_row:
                # request_id = log_string_row.split(" ")[16].split(":")[1][1:-2]
                request_id = log_string_row.split(" ")[14].split(":")[1][1:-2]
                if node_name_row != "-" and not isinstance(pod_name_row, float):
                    request_id_to_pod_map[request_id] = (pod_name_row, node_name_row)
                else:
                    request_id_to_pod_map[request_id] = (pod_name_row, None)
    # for each row extract the log message that needs to be parsed
    count = 0
    for _, row in input_df.iterrows():
        # get request_id to pod and node mapping from log data for the application pod
        log_string_row = row["log"]
        pod_name_row = row["kubernetes.pod_name"]
        node_name_row = row["kubernetes.host"]

        pod_name_pattern = r'\b[a-zA-Z0-9-]+-deployment-[a-f0-9]+-[a-z0-9]+\b'
        if pod_name_row != np.nan and not isinstance(pod_name_row, float):
            pod_name_match = re.search(pod_name_pattern, pod_name_row)

            if pod_name_match and not isinstance(log_string_row, float) and "[queue-proxy]" in log_string_row:
                count += 1
                request_id = log_string_row.split(" ")[44].split(":")[1][1:-2]
                if request_id not in request_id_to_pod_map:
                    if node_name_row != "-" and not isinstance(pod_name_row, float):
                        request_id_to_pod_map[request_id] = (pod_name_row, node_name_row)
                    else:
                        # This is not expected so far as if a pod does not exist then it cannot be allocated to a node
                        request_id_to_pod_map[request_id] = (pod_name_row, None)
                else:
                    # This is not expected so far as if a pod does not exist then it cannot be allocated to a node
                    if request_id_to_pod_map[request_id][1] == None and node_name_row != "-":
                        request_id_to_pod_map[request_id] = (pod_name_row, node_name_row)
    for _, row in input_df.iterrows():
        log_string_row = row["log"]
        # log pattern - python-requests/a.b.c but not [python-requests/a.b.c]
        request_clause = r"(?<!\[)python-requests/\d+\.\d+\.\d+(?!\])"
        
        if not isinstance(log_string_row, float) and re.search(request_clause, log_string_row):
            log_string_list = log_string_row.split(" ")
            # accept only if we have 200 OK in our data
            start_time_list = log_string_list[0][1:-1].split('T')
            start_time = f'{start_time_list[0]}T{start_time_list[1][:-1]}'
            start_time_dt = datetime.fromisoformat(start_time)

            request_id = log_string_list[12][1:-1]
            request_duration = log_string_list[8]
            # request_duration = int(log_string_list[8])
            request_id_to_ingress_start_map[request_id] = (start_time_dt, request_duration)
            request_id_to_app_map[request_id] = log_string_list[13][1:21]
            request_id_to_response_type_map[request_id] = "SUCCESS" if int(log_string_list[4]) == 200 else "FAILED"
        
        if not isinstance(log_string_row, float) and "[queue-proxy]" in log_string_row:
            log_string_list = log_string_row.split(" ")
            end_date = log_string_list[6]
            end_time = log_string_list[7]
            nanosec = end_time.split('.')[1]
            rounded_time = end_time.split('.')[0]

            request_id = log_string_list[44].split(":")[1][1:-2]
            # TODO: add back nanoseconds
            end_time_unformatted = f'{end_date}T{rounded_time + "." + nanosec[:3]}'
            end_time_dt = datetime.fromisoformat(end_time_unformatted)
            request_id_to_queue_proxy_end_map[request_id] = end_time_dt

    # get provisioning duration
    request_id_to_duration_map = {}
    for request_id, start_time_duration in request_id_to_ingress_start_map.items():
        if request_id in request_id_to_queue_proxy_end_map:
            # lets just put a min cap of 0.1 so that nano seconds dont become 0ms
            request_id_to_duration_map[request_id] = max(timedelta(milliseconds=1), request_id_to_queue_proxy_end_map[request_id] - start_time_duration[0])
        # request_id_to_duration_map[request_id] = max(timedelta(milliseconds=1), timedelta(milliseconds=start_time_duration[1]))
    
    output_df = pd.DataFrame({
    "REQUEST_ID": [request_id for request_id in request_id_to_ingress_start_map.keys()],
    "APP_NAME" : [request_id_to_app_map[req_id] if req_id in request_id_to_app_map else None for req_id in request_id_to_ingress_start_map.keys()],
    "POD_NAME": [request_id_to_pod_map[req_id][0] if req_id in request_id_to_pod_map else None for req_id in request_id_to_ingress_start_map.keys()],
    "NODE_NAME": [request_id_to_pod_map[req_id][1] if req_id in request_id_to_pod_map else None for req_id in request_id_to_ingress_start_map.keys()],
    "REQUEST_START_TIME": [start_time_and_duration[0] for request_id, start_time_and_duration in request_id_to_ingress_start_map.items()],
    "REQUEST_DURATION": [request_id_to_duration_map[request_id].total_seconds()*1000 if request_id in request_id_to_duration_map else None for request_id, _ in request_id_to_ingress_start_map.items()],
    "REQUEST_RESPONSE_TYPE": [request_id_to_response_type_map[req_id] if req_id in request_id_to_response_type_map else "SOMETHING" for req_id in request_id_to_response_type_map.keys()]
    })

    # sort it on the basis of request id
    output_df = output_df.sort_values(by="REQUEST_START_TIME")
    output_df = output_df.reset_index(drop=True)
    print("request info: \n", output_df.to_string())
    output_df.to_pickle(OUTPUT_FOLDER + "actual_request_id_start_and_end_df.pickle")