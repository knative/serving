import math
import sys
import pandas as pd

# df = pd.read_pickle("../../application_deployment_automation/data/max_meta_func_deployment_150/dataframe/workload_event_data_max_72_150_apps.pickle")
# df = pd.read_pickle("../../application_deployment_automation/data/mid_meta_func_deployment_100/dataframe/workload_event_data_max_72_100_apps.pickle")
df = pd.read_pickle("../../application_deployment_automation/data/meta_func_deployment_100/dataframe/workload_event_data_max_63_100_apps.pickle")



# create function hash to exec time mapping
hash_to_exec_map = {}
for ix, row in df.iterrows():
    hash_to_exec_map[row["HashApp"]] = row["ExecDurations"]


application_names_to_mem = {"application-syntheti": 277, "application-synthetic": 277}
for ix, row in df.iterrows():
    application_names_to_mem[f"application-{row['HashApp'][:8]}"] = row["AverageMemUsage"]


def get_int_from_time_str(time_str):
    time_comp = time_str.split(":")

    hrs = int(time_comp[0])
    min = int(time_comp[1])
    sec_and_mic = time_comp[2].split(".")
    sec = int(sec_and_mic[0])

    if len(sec_and_mic) > 1:
        mic = int(sec_and_mic[1]) * (math.pow(10, 6 - len(sec_and_mic[1])))
    else:
        mic = 0

    # Calculate the total seconds
    total_sec = (
        hrs * 3600
        + min * 60  # 1 hour = 3600 seconds
        + sec  # 1 minute = 60 seconds
        + mic / 1e6  # 1 microsecond = 1e-6 seconds
    )

    # Convert total_seconds to an integer
    converted_int = float(total_sec)
    return converted_int

def hash_to_app(unique_app_list):
    app_to_exec_map = {"application-syntheti": 131.5}
    for key, val in hash_to_exec_map.items():
        for app in unique_app_list:
            if key[:8] in app:
                app_to_exec_map[app] = val 
    return app_to_exec_map


def app_to_exec(app):
    for key, val in hash_to_exec_map.items():
        if key[:8] in app:
            return val 
    return None