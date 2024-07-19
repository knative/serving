import os
import time
import math
import subprocess
import pandas as pd
import matplotlib.pyplot as plt
from datetime import datetime, timezone

from helper import application_names_to_mem

# get resource utilization across all pods
def get_resource_utilization(command_phrase, node_mode, pod_mode, cushion_memory, per_pod_concurrency, cpu_alloc):
    """ Log resource usage and associated UTC timestamp in seconds

    returns:
        cpu : int (for pods) and [int, int, int] (for nodes)
            for pods, the int represents sum of usage in millicores.
            for nodes, the ints represent utilization percentage with i=0 for the control nodes, 
            i=1 for max worker, i=2 for min worker

        mem : int (for pods) and [int, int, int] (for nodes)
            same as cpu but with MiB
        
        time: float
    """
    
    # the command
    command = f"kubectl top {command_phrase}"

    # get the output from the command
    output = subprocess.check_output(command, shell=True, text=True)

    # split into CPU and Memory

    if output == "No resources found in default namespace." or "error" in output:
        print("no pods are up!")
        return 0, 0, time.time()

    # remove first line as it only provides column name data
    line_output = output.split("\n")[1:-1]

    app_name_to_pod_count_map = {}

    if node_mode:
        # format: names  CPU usage (millicore)  CPU %  Memory usage (B)  Memory %
        control_cpu_util_pct = None
        control_mem_util_pct = None
        max_cpu_util_pct = 0
        max_mem_util_pct = 0
        min_cpu_util_pct = 100
        min_mem_util_pct = 100
        
        for line in line_output:
            metric_list = line.split()
            
            if metric_list[0] == "node1":
                control_cpu_util_pct = int(metric_list[2][:-1])
                control_mem_util_pct = int(metric_list[4][:-1])

            else:
                max_cpu_util_pct = max(int(metric_list[2][:-1]), max_cpu_util_pct)
                max_mem_util_pct = max(int(metric_list[4][:-1]), max_mem_util_pct)
                
                min_cpu_util_pct = min(int(metric_list[2][:-1]), min_cpu_util_pct)
                min_mem_util_pct = min(int(metric_list[4][:-1]), min_mem_util_pct)
        
        cpu = [control_cpu_util_pct, max_cpu_util_pct, min_cpu_util_pct]
        memory = [control_mem_util_pct, max_mem_util_pct, min_mem_util_pct]

    else:
        # for each line in the list this is the format:
        # namespace    pod-name    cpu(millicores)    memory(MiB)
        # therefore, we extract 3rd and 4th column only
        util_cpu = 0
        util_memory = 0

        for line in line_output:
            if "user-container" not in line:
                continue

            metric_list = line.split()
            if len(metric_list) != 0:
                # get the resource metric and convert it to integer
                util_cpu += int(metric_list[-2][:-1])
                util_memory += int(metric_list[-1][:-2])

                # store {app name : [cpu, mem, # pods]}
                app_name = metric_list[0][:20]

                if app_name in app_name_to_pod_count_map:
                    app_name_to_pod_count_map[app_name] += 1
                else:
                    app_name_to_pod_count_map[app_name] = 1
        # print("app_map: ", app_name_to_pod_count_map)
        alloc_cpu = 0
        alloc_memory = 0
        for app_name, pod_count in app_name_to_pod_count_map.items():
            alloc_cpu += (cpu_alloc)*pod_count 
            alloc_memory += (application_names_to_mem[app_name]*per_pod_concurrency + cushion_memory)*pod_count
        cpu = [util_cpu, alloc_cpu]
        memory = [util_memory, alloc_memory]
    return cpu, memory, datetime.now(timezone.utc).timestamp(), app_name_to_pod_count_map
    
def get_application_allocation(app_to_util, cushion_memory):
    memory_wasted_across_apps = {}
    for key, val in app_to_util.items():
        
        # get number of pods per function
        number_of_pods_per_app = val[2]
        memory_utilization_per_app = val[1]
        
        # multiply with actual allocation per pod
        memory_allocated_per_pod = application_names_to_mem[key] + cushion_memory
        memory_allocated_per_app = memory_allocated_per_pod * number_of_pods_per_app

        # subtract allocated from actual usage
        memory_wasted_per_app = memory_allocated_per_app - memory_utilization_per_app

        # TODO: remove this check once the cushion_memory has a fixed value
        if memory_wasted_per_app < 0:
            raise Exception("wasted memory cannot be negative") 

        if key in memory_wasted_across_apps:
            memory_wasted_across_apps[key] += memory_wasted_per_app
        else:
            memory_wasted_across_apps[key] = memory_wasted_per_app
    return memory_wasted_across_apps

def collect_resource_utilization(command_substring, kubernetes_resource_type, pod_mode, cushion_memory, per_pod_concurrency, cpu_alloc, OUTPUT_FOLDER):
    """Store memory and cpu usage with associated UTC timestamp in seconds.
    """
    node_mode = True if "node" in command_substring else False
    if node_mode:
        df = pd.DataFrame(columns=['COLLECTION_TIME', 'CPU_PCT', 'MAX_CPU_PCT', 'MIN_CPU_PCT', 'MEM_PCT', 'MAX_MEM_PCT', 'MIN_MEM_PCT'])
    else:
        df = pd.DataFrame(columns=['COLLECTION_TIME', 'CPU_UTIL', 'CPU_ALLOC', 'MEM_UTIL', 'MEM_ALLOC'])
    cpu_list = []
    memory_list = []
    collection_time_list = []
    save_file_timer = 30 #s
    iter = 0
    try:
        while True:
            cpu, memory, collection_time, app_name_to_pod_count_map = get_resource_utilization(command_substring, node_mode, pod_mode, cushion_memory, per_pod_concurrency, cpu_alloc)
            if node_mode:
                current_entry = {'COLLECTION_TIME': collection_time, 'CPU_PCT': cpu[0], 'MAX_CPU_PCT': cpu[1], 'MIN_CPU_PCT': cpu[2], 'MEM_PCT': memory[0], 'MAX_MEM_PCT': memory[1], 'MIN_MEM_PCT': memory[2]}
            else:
                current_entry = {'COLLECTION_TIME': collection_time, 'CPU_UTIL': cpu[0], 'CPU_ALLOC': cpu[1], 'MEM_UTIL': memory[0], 'MEM_ALLOC': memory[1], 'POD_COUNT': sum([pod_count_per_app for pod_count_per_app in app_name_to_pod_count_map.values()])}

            df = pd.concat([df, pd.DataFrame([current_entry])], ignore_index=True)
            print(cpu)
            print(memory)
            print(collection_time)
            print(app_name_to_pod_count_map)
            cpu_list.append(cpu)
            memory_list.append(memory)
            collection_time_list.append(collection_time)
            # sleep for a 1 second
            time.sleep(1)
            iter += 1
            if not os.path.exists(OUTPUT_FOLDER):
                print("creating directory structure...")
                os.makedirs(OUTPUT_FOLDER)
            if iter == save_file_timer:
                iter = 0
                if node_mode:
                    df.to_pickle(OUTPUT_FOLDER + "node_resource_data.pickle")
                else:
                    df.to_pickle(OUTPUT_FOLDER + "pod_resource_data.pickle")

            

    except KeyboardInterrupt:
        print("\nStopped Monitoring...")
        

if __name__ == "__main__":
    cushion_memory = 50 # MB
    per_pod_concurrency = 1
    cpu_alloc = 350 # milli cores
    pod_mode = "utilization" # utilization or allocation

    CURRENT_RUN_KEYWORD = "femux_v3_mc"
    OUTPUT_FOLDER = f"../output/{CURRENT_RUN_KEYWORD}/"

    collect_resource_utilization("pods --containers=true", "all namespaces (c15)", pod_mode, cushion_memory, per_pod_concurrency, cpu_alloc, OUTPUT_FOLDER)
    # collect_resource_utilization("nodes", "all namespaces (c15)", pod_mode, cushion_memory, per_pod_concurrency, cpu_alloc, OUTPUT_FOLDER)