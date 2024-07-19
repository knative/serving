import pandas as pd
from datetime import datetime
import matplotlib.pyplot as plt
import numpy as np

# we are going to use pandas and string regex ops to extract data
def get_int_from_time_str(time_str):
    time_comp = time_str.split(":")

    hrs = int(time_comp[0])
    min = int(time_comp[1])
    sec_and_mic = time_comp[2].split(".")
    sec = int(sec_and_mic[0])
    if len(sec_and_mic) > 1:
        mic = int(sec_and_mic[1])
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
    converted_int = int(total_sec)
    return converted_int


def get_pod_name_start_and_end_data(DATA_FOLDER, OUTPUT_FOLDER):
    # files, folders, constants etc.
    DATA_START_FILE = "pod_name_start_time.csv"
    DATA_END_FILE = "pod_name_end_time.csv"

    input_start_df = pd.read_csv(DATA_FOLDER + DATA_START_FILE)
    input_end_df = pd.read_csv(DATA_FOLDER + DATA_END_FILE)

    # pod_name: [start, end]
    pod_name_to_start_and_end_time_map = {}

    output_df = pd.DataFrame()

    for _, row in input_start_df.iterrows():
        log_string_row = row["log"]
        log_string_list_with_empty_elements = log_string_row.split(" ")
        log_string_list = [
            str for str in log_string_list_with_empty_elements if str.strip()
        ]
        if log_string_list[0] == "pod":
            pod_name = log_string_list[2]
            start_date = log_string_list[8]
            start_time = log_string_list[9]
            if pod_name not in pod_name_to_start_and_end_time_map:
                pod_name_to_start_and_end_time_map[pod_name] = [0, 0, 0, 0]
                start_datetime_str = f'{start_date}T{start_time}'
                start_datetime = datetime.fromisoformat(start_datetime_str)

                pod_name_to_start_and_end_time_map[pod_name][0] = start_datetime
                # pod_name_to_start_and_end_time_map[pod_name][2] = get_int_from_time_str(start_time)
    # for each row extract the log message that needs to be parsed
    for ix, row in input_end_df.iterrows():
        # for each log message extract:
        log_string_row = row["log"]
        log_string_list_with_empty_elements = log_string_row.split(" ")
        log_string_list = [
            str for str in log_string_list_with_empty_elements if str.strip()
        ]

        # pod name - this will come at least once so at the end of the loop we need to add start and end time to the output_df
        pod_name = log_string_list[-1][:-1]
        if "-deployment-" in pod_name:
            end_time = log_string_list[1]
            end_time_int = get_int_from_time_str(log_string_list[1])
            if pod_name in pod_name_to_start_and_end_time_map:
                log_string_row = row["@timestamp"]
                end_date = log_string_row.split("T")[0]
                end_datetime_str = f'{end_date}T{end_time}'
                end_datetime = datetime.fromisoformat(end_datetime_str)
                pod_name_to_start_and_end_time_map[pod_name][1] = end_datetime
                # pod_name_to_start_and_end_time_map[pod_name][3] = end_time_int
    output_df = pd.DataFrame(
        {
            "POD_NAME": pod_name_to_start_and_end_time_map.keys(),
            "POD_START_TIME": [
                start_end_time[0]
                for start_end_time in pod_name_to_start_and_end_time_map.values()
            ],
            "POD_END_TIME": [
                start_end_time[1]
                for start_end_time in pod_name_to_start_and_end_time_map.values()
            ],
            "POD_DURATION": [
                (start_end_time[1] - start_end_time[0]).total_seconds()*1000
                if start_end_time[0] != 0 and start_end_time[1] != 0
                else -1
                for start_end_time in pod_name_to_start_and_end_time_map.values()
            ],
        }
    )
    output_df['POD_END_TIME'] = pd.to_datetime(output_df['POD_END_TIME'], format='%Y-%m-%d %H:%M:%S.%f', errors='coerce')
    output_df['POD_START_TIME'] = pd.to_datetime(output_df['POD_START_TIME'], format='%Y-%m-%d %H:%M:%S.%f', errors='coerce')

    output_df = output_df.sort_values(by="POD_START_TIME")
    output_df = output_df.reset_index(drop=True)
    print("pod info: \n", output_df.to_string())
    output_df.to_pickle(OUTPUT_FOLDER + "pod_name_start_and_end_df.pickle")


# DATA_FOLDER = "../data/kibana_csv/simulation_cs_vanilla_1_cpu_2.6_mem_1_ppc_2_rpc_hl_1/"
# OUTPUT_FOLDER = "../output/simulation_cs_vanilla_1_cpu_2.6_mem_1_ppc_2_rpc_hl_1/"

# get_pod_name_start_and_end_data(DATA_FOLDER, OUTPUT_FOLDER)