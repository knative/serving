import pandas as pd
import numpy as np
import matplotlib.pyplot as plt

COUNTER = 0

def get_request_time_distribution(data_folder, data_file_name, output_folder, output_file_name):

    data_df = pd.read_csv(data_folder + data_file_name)
    print("starting time distribution")

    df = pd.DataFrame(columns=["IngressStartTime", "IngressDuration", "QPStartTime", "QPDuration", "QPEndTime", "APP_NAME"])
    counter = 0
    df_list = []
    df_row_map = {}
    for _, row in data_df.iterrows():   
        if not isinstance(row['log'], float): 
            log_line = row['log']
            log_line = log_line.split(" ")
            if '\"POST' in log_line:
                req_hash = log_line[12].replace('"',"")
                app_name = log_line[13].split('.')[0][1:]
                if req_hash not in df_row_map:
                    df_row_map[req_hash] = {}
                    df_row_map[req_hash].update({
                                    "IngressStartTime": log_line[0][1:-1], 
                                    "IngressDuration": log_line[8], 
                                    "APP_NAME": app_name})
                else:
                    df_row_map[req_hash].update({
                                    "IngressStartTime": log_line[0][1:-1], 
                                    "IngressDuration": log_line[8], 
                                    "APP_NAME": app_name})


            if '[queue-proxy]' in log_line[0]:
                req_hash = log_line[44].split(":")[1][1:-2]
                if req_hash not in df_row_map:
                    df_row_map[req_hash] = {}
                    df_row_map[req_hash].update({
                                   "QPStartTime": f"{log_line[6]} {log_line[7]}", 
                                   "QPDuration": log_line[25], 
                                   "QPEndTime": f"{log_line[16]} {log_line[17]}"})
                else:
                    df_row_map[req_hash].update({
                                   "QPStartTime": f"{log_line[6]} {log_line[7]}", 
                                   "QPDuration": log_line[25], 
                                   "QPEndTime": f"{log_line[16]} {log_line[17]}"})
            
        counter += 1
        if counter % 10000 == 0:
            print(f"processed {counter} rows")
    # change this df_row_map to dataframe with main key as req_hash and keys in the value map as columns and values as values
    for _, value in df_row_map.items():
        df_list.append(value)
    df = pd.DataFrame(df_list)
    print(df.to_string())
    plt.scatter([i for i in range(len(df))], [val if val != np.nan else 0 for val in df["QPDuration"].astype(float).to_list()])
    plt.savefig(output_folder + "exec_duration.pdf")
    df.to_pickle(output_folder + output_file_name)
    print("finished time distribution")
# df = pd.read_pickle("../output/resource_data_simulation_cs_vanilla/pod_resource_data1.pickle")

# print(df.to_string())