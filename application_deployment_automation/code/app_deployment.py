import pandas as pd
import matplotlib.pyplot as plt
import numpy as np
import subprocess
import json

import os
from os import listdir
from os.path import isfile, join
from app_yaml_templates import mod_file, docker_file, application_file

local_registry_url = "206.12.96.137:5000"
iat_workload_data = "../data/meta_func_deployment_100/dataframe/workload_event_data_max_63_100_apps.pickle"
image_url_template = "{}/application-{}.go:latest"
app_url_template = "application-{}.default.192.168.106.10.sslip.io"


def deploy_apps(workload_scaling_factor):
    # df = pd.read_pickle(iat_workload_data)
    # df = scale_workload(df, workload_scaling_factor)
    # # create_go_modules(df)

    subprocess.run(["sh", "deploy_apps.sh"])

    # gen_faas_profiler(df)

    return df


def scale_workload(df, scaling_factor):
    df["ExecDurations"] = df["ExecDurations"].apply(lambda x: scale_exec_duration(x, scaling_factor))
    df["IAT"] = df["IAT"].apply(lambda x: scale_each_trace(x, scaling_factor))
    return df

# scale exec time by 2
def scale_exec_duration(duration, scaling_factor):
    return duration / scaling_factor

# scale IAT by 2
def scale_each_trace(iat_list, scaling_factor):
    if len(iat_list) == 0:
        return iat_list
    else: 
        return [iat/scaling_factor for iat in iat_list]

def create_go_modules(df):
    # create go modules, class with correct avg exec and mem alloc and go mod for ko
    for index, row in df.iterrows():
        code_folder = f'../data/meta_func_deployment_100/apps/{row.HashApp}'
        if not os.path.exists(code_folder):
            print("creating directory structure...")
            os.makedirs(code_folder)
            # first get all lines from file
    
        with open(code_folder + "/" + 'go.mod', 'w') as fp:
            fp.write(str(mod_file['go.mod'].format(row.HashApp)))
        fp.close()

        with open(code_folder + "/" + 'application_{}.go'.format(str(row.HashApp)[:8]), 'w') as fp:
            fp.write(str(application_file.format(row.ExecDurations, row.AverageMemUsage)))
        fp.close()

        with open(code_folder + "/" + 'Dockerfile', 'w') as fp:
            fp.write(str(docker_file["Dockerfile"]))
        fp.close()
        with open(code_folder + "/" + str(row.HashApp)+"_output.txt", 'w') as fp:
            fp.write("")
        fp.close()


def gen_faas_profiler(complete_df):
    # iat generation
    with open('../data/misc/workload_configs_default.json', 'r+') as f:
        data = json.load(f)
        number_of_instances = len(complete_df)
        overall_list = np.array([])
        index = 0
        for _, row in complete_df.iterrows():
            instance_structure = {
                "application": f"application-{row['HashApp'][:8]}",
                "url": "http://127.0.0.1:8080/hello",
                "host": f"{app_url_template.format(row['HashApp'][:8])}",
                "data": {},
                "interarrivals_list": []
            }
            instance_structure["interarrivals_list"] = list(row["IAT"])
            data["instances"][f"instance_{index}"] = instance_structure
            index += 1
    
    f = open('../data/misc/new_json_files/work_load_configs_rep_meta_func_IAT.json', 'x')
    json.dump(data, f, indent=4)


if __name__ == "__main__":
    scaling_factor = 1
    deploy_apps(scaling_factor)